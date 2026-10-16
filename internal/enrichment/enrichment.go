// Package enrichment adds privacy-preserving derived fields and optional
// MaxMind GeoIP data to successfully parsed events.
package enrichment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

// Config names the exact MMDB paths used by the optional GeoIP stages.
// Missing files are tolerated so an RPM can install before an administrator
// provisions licensed GeoLite databases; present but invalid files fail closed.
type Config struct {
	CountryDB string `json:"country_db,omitempty"`
	ASNDB     string `json:"asn_db,omitempty"`
	CityDB    string `json:"city_db,omitempty"`
}

// ValidateConfig validates configured paths without requiring database files
// to exist yet.
func ValidateConfig(config Config) error {
	for name, path := range map[string]string{"country_db": config.CountryDB, "asn_db": config.ASNDB, "city_db": config.CityDB} {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return fmt.Errorf("%s must be an absolute path without control characters", name)
		}
	}
	return nil
}

// Enricher is safe for concurrent lookups. MMDB Reader supports concurrent
// reads; close only after the agent's parsing workers have stopped.
type Enricher struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader
	city    *maxminddb.Reader
}

// Open opens configured MMDB files. Nonexistent files are skipped to support
// an install-first/provision-data-later deployment. Existing malformed or
// unreadable files return an error so bad GeoIP configuration is visible.
func Open(config Config) (*Enricher, error) {
	if err := ValidateConfig(config); err != nil {
		return nil, err
	}
	out := &Enricher{}
	for _, item := range []struct {
		name string
		path string
		dest **maxminddb.Reader
	}{
		{"country", config.CountryDB, &out.country},
		{"ASN", config.ASNDB, &out.asn},
		{"city", config.CityDB, &out.city},
	} {
		if item.path == "" {
			continue
		}
		if _, err := os.Stat(item.path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			_ = out.Close()
			return nil, fmt.Errorf("stat %s MMDB %q: %w", item.name, item.path, err)
		}
		reader, err := maxminddb.Open(item.path)
		if err != nil {
			_ = out.Close()
			return nil, fmt.Errorf("open %s MMDB %q: %w", item.name, item.path, err)
		}
		*item.dest = reader
	}
	if out.country == nil && out.asn == nil && out.city == nil {
		return nil, nil
	}
	return out, nil
}

func (e *Enricher) Close() error {
	if e == nil {
		return nil
	}
	var first error
	for _, reader := range []*maxminddb.Reader{e.country, e.asn, e.city} {
		if reader != nil {
			if err := reader.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// Apply derives bucket fields from common mail-log keys, then adds GeoIP
// values when a configured MMDB contains a public-IP record. The raw source
// fields remain customer data; bucket values are derived fields and should be
// used for grouping instead of exporting raw IP/email values as labels.
func Apply(fields map[string]interface{}, geo *Enricher) {
	if fields == nil {
		return
	}
	if user := firstString(fields, "user", "username", "userid", "email"); user != "" {
		if bucket := emailBucket(user); bucket != "" {
			fields["email_bucket"] = bucket
		}
	}
	ip := parseIP(firstString(fields, "fromhost", "remoteaddr", "client_address", "clientaddr", "ip"))
	if ip == nil {
		return
	}
	fields["is_private_ip"] = isPrivateIP(ip)
	if bucket := networkBucket(ip); bucket != "" {
		fields["network_bucket"] = bucket
	}
	if geo == nil || isPrivateIP(ip) {
		return
	}
	if geo.country != nil {
		var record countryRecord
		if err := geo.country.Lookup(ip, &record); err == nil {
			putString(fields, "geoip_country_code", record.Country.ISOCode)
			putString(fields, "geoip_country_name", record.Country.Names["en"])
			putString(fields, "geoip_continent_code", record.Continent.Code)
			putString(fields, "geoip_continent_name", record.Continent.Names["en"])
		}
	}
	if geo.asn != nil {
		var record asnRecord
		if err := geo.asn.Lookup(ip, &record); err == nil {
			if record.Number != 0 {
				fields["geoip_autonomous_system_number"] = record.Number
			}
			if record.Organization != "" {
				fields["geoip_autonomous_system_organization"] = record.Organization
			}
		}
	}
	if geo.city != nil {
		var record cityRecord
		if err := geo.city.Lookup(ip, &record); err == nil {
			putString(fields, "geoip_city_name", record.City.Names["en"])
			if fields["geoip_country_code"] == nil {
				putString(fields, "geoip_country_code", record.Country.ISOCode)
			}
			if fields["geoip_country_name"] == nil {
				putString(fields, "geoip_country_name", record.Country.Names["en"])
			}
			if fields["geoip_continent_code"] == nil {
				putString(fields, "geoip_continent_code", record.Continent.Code)
			}
			if fields["geoip_continent_name"] == nil {
				putString(fields, "geoip_continent_name", record.Continent.Names["en"])
			}
			if record.Postal.Code != "" {
				fields["geoip_postal_code"] = record.Postal.Code
			}
			if record.Location.TimeZone != "" {
				fields["geoip_timezone"] = record.Location.TimeZone
			}
			if len(record.Subdivisions) > 0 {
				putString(fields, "geoip_subdivision_name", record.Subdivisions[0].Names["en"])
				putString(fields, "geoip_subdivision_code", record.Subdivisions[0].ISOCode)
			}
			if record.Location.Latitude != 0 {
				fields["geoip_latitude"] = record.Location.Latitude
			}
			if record.Location.Longitude != 0 {
				fields["geoip_longitude"] = record.Location.Longitude
			}
		}
	}
}

type countryRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Continent struct {
		Code  string            `maxminddb:"code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
}

type asnRecord struct {
	Number       uint   `maxminddb:"autonomous_system_number"`
	Organization string `maxminddb:"autonomous_system_organization"`
}

type cityRecord struct {
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Continent struct {
		Code  string            `maxminddb:"code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Postal struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"postal"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		TimeZone  string  `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

func putString(fields map[string]interface{}, name, value string) {
	if value != "" {
		fields[name] = value
	}
}

func firstString(fields map[string]interface{}, names ...string) string {
	for _, name := range names {
		if value, ok := fields[name].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func emailBucket(value string) string {
	local, _, found := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "@")
	if !found || local == "" {
		return ""
	}
	local = strings.TrimSuffix(local, ".t21")
	local = strings.TrimSuffix(local, ".t1")
	if local == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(local))
	return hex.EncodeToString(digest[:])[:2]
}

func parseIP(value string) net.IP {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return net.ParseIP(strings.Trim(host, "[]"))
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func networkBucket(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		prefix := v4.Mask(net.CIDRMask(24, 32))
		return hashPrefix(prefix.String(), 1)
	}
	v6 := ip.To16()
	if v6 == nil {
		return ""
	}
	prefix := v6.Mask(net.CIDRMask(64, 128))
	return hashPrefix(prefix.String(), 1)
}

func hashPrefix(value string, chars int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if chars > len(encoded) {
		chars = len(encoded)
	}
	return encoded[:chars]
}
