package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nexora.local/nli-v13/internal/fleet"
	"nexora.local/nli-v13/internal/middleware"
	metastore "nexora.local/nli-v13/internal/store"
)

var parserAssetCollections = []string{"kv-config", "static-patterns", "projections", "templates", "regex-patterns", "known-keys", "semantic-extraction"}

type parserBundleAsset struct {
	AssetClass string          `json:"asset_class"`
	AssetID    string          `json:"asset_id"`
	Name       string          `json:"name"`
	Revision   int             `json:"revision"`
	SHA256     string          `json:"sha256"`
	Payload    json.RawMessage `json:"payload"`
}
type parserAssetBundle struct {
	SchemaVersion int                 `json:"schema_version"`
	Assets        []parserBundleAsset `json:"assets"`
	BundleHash    string              `json:"bundle_hash"`
}

func bundleDigest(bundle parserAssetBundle) (string, []byte, error) {
	bundle.BundleHash = ""
	unsigned, err := json.Marshal(struct {
		SchemaVersion int                 `json:"schema_version"`
		Assets        []parserBundleAsset `json:"assets"`
	}{bundle.SchemaVersion, bundle.Assets})
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(unsigned)
	return hex.EncodeToString(digest[:]), unsigned, nil
}

func (s *Server) buildActiveParserBundle(ctx context.Context) (parserAssetBundle, error) {
	bundle := parserAssetBundle{SchemaVersion: 1, Assets: []parserBundleAsset{}}
	for _, collection := range parserAssetCollections {
		page, err := s.metadata.List(ctx, collection, metastore.Query{Limit: 1000})
		if err != nil {
			return bundle, err
		}
		for _, doc := range page {
			if doc.State != "active" || doc.Deleted {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(doc.Payload, &payload); err != nil {
				return bundle, fmt.Errorf("invalid active parser asset %s/%s", collection, doc.ID)
			}
			name, _ := payload["name"].(string)
			bundle.Assets = append(bundle.Assets, parserBundleAsset{AssetClass: collection, AssetID: doc.ID, Name: name, Revision: doc.Revision, SHA256: doc.Hash, Payload: append(json.RawMessage(nil), doc.Payload...)})
		}
	}
	// Stable order makes the same approved asset set produce the same bundle hash.
	sort.Slice(bundle.Assets, func(i, j int) bool {
		if bundle.Assets[i].AssetClass == bundle.Assets[j].AssetClass {
			return bundle.Assets[i].AssetID < bundle.Assets[j].AssetID
		}
		return bundle.Assets[i].AssetClass < bundle.Assets[j].AssetClass
	})
	hash, _, err := bundleDigest(bundle)
	bundle.BundleHash = hash
	return bundle, err
}

func (s *Server) materializeParserBundle(bundle parserAssetBundle) (string, error) {
	hash, _, err := bundleDigest(bundle)
	if err != nil {
		return "", err
	}
	if hash != bundle.BundleHash {
		return "", errors.New("parser bundle hash calculation failed")
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.cfg.StateDir, "parser-assets")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "bundle.json")
	temp := path + ".staging"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Rename(temp, path); err != nil {
		return "", err
	}
	readback, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var actual parserAssetBundle
	if err = json.Unmarshal(readback, &actual); err != nil {
		return "", err
	}
	readHash, _, err := bundleDigest(actual)
	if err != nil || readHash != bundle.BundleHash {
		return "", errors.New("controller parser bundle readback hash mismatch")
	}
	return readHash, nil
}

func (s *Server) startParserBundleRollout(ctx context.Context, actor, name string, bundle parserAssetBundle, targetIDs []string) (*rolloutRecord, error) {
	if len(targetIDs) == 0 {
		for _, node := range s.fleet.All() {
			if node.Status != fleet.StatusOffline {
				targetIDs = append(targetIDs, node.ID)
			}
		}
	}
	if len(targetIDs) == 0 {
		return nil, nil
	}
	payload := map[string]any{"asset_bundle": bundle, "bundle_hash": bundle.BundleHash}
	rollout, err := s.rollouts.Create(rolloutTypeParserSync, name, actor, targetIDs, payload)
	if err != nil {
		return nil, err
	}
	if err := s.rollouts.Start(rollout.ID, targetIDs); err != nil {
		return nil, err
	}
	for _, nodeID := range targetIDs {
		s.updateDesiredNodeAssetSync(ctx, nodeID, bundle)
	}
	item, ok := s.rollouts.Get(rollout.ID)
	if !ok {
		return nil, errors.New("created parser sync rollout disappeared")
	}
	return item, nil
}

func (s *Server) updateDesiredNodeAssetSync(ctx context.Context, nodeID string, bundle parserAssetBundle) {
	id := safeID(nodeID)
	doc, _ := s.metadata.Get(ctx, "fleet-sync", id, 0)
	state := map[string]any{"node_id": nodeID}
	_ = json.Unmarshal(doc.Payload, &state)
	revisions := map[string]int{}
	for _, asset := range bundle.Assets {
		key := asset.AssetClass + "/" + asset.AssetID
		revisions[key] = asset.Revision
	}
	state["desired_asset_bundle_hash"] = bundle.BundleHash
	state["desired_asset_revisions"] = revisions
	state["desired_asset_at"] = time.Now().UTC()
	state["asset_drifted"] = fmt.Sprint(state["active_asset_bundle_hash"]) != bundle.BundleHash
	state["asset_sync_status"] = "pending"
	payload, _ := json.Marshal(state)
	_, _ = s.metadata.Put(ctx, metastore.Document{Collection: "fleet-sync", ID: id, State: "desired", Payload: payload, CreatedBy: doc.CreatedBy})
}

func (s *Server) handleParserSync(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/parser-sync"), "/")
	if path == "" && r.Method == http.MethodGet {
		if s.authorize(w, r, middleware.PermissionReadFleet) == nil {
			return
		}
		s.listParserSync(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		jsonError(w, "GET or POST required", http.StatusMethodNotAllowed)
		return
	}
	if path != "verify" && path != "retry" {
		jsonError(w, "action must be verify or retry", http.StatusNotFound)
		return
	}
	permission := middleware.PermissionReadParsers
	if path == "retry" {
		permission = middleware.PermissionTriggerRollout
	}
	principal := s.authorize(w, r, permission)
	if principal == nil {
		return
	}
	var request struct {
		AssetClass string   `json:"asset_class"`
		AssetID    string   `json:"asset_id"`
		NodeIDs    []string `json:"node_ids,omitempty"`
	}
	if err := decodeJSON(r, &request, 64<<10); err != nil {
		jsonError(w, "invalid parser sync action", http.StatusBadRequest)
		return
	}
	bundle, err := s.buildActiveParserBundle(r.Context())
	if err != nil {
		jsonError(w, "active parser bundle unavailable", http.StatusServiceUnavailable)
		return
	}
	if request.AssetClass != "" || request.AssetID != "" {
		found := false
		for _, asset := range bundle.Assets {
			if asset.AssetClass == request.AssetClass && asset.AssetID == request.AssetID {
				found = true
				break
			}
		}
		if !found {
			jsonError(w, "active asset not found", http.StatusNotFound)
			return
		}
	}
	controllerHash, err := s.materializeParserBundle(bundle)
	if err != nil {
		jsonError(w, "controller parser bundle materialization failed", http.StatusServiceUnavailable)
		return
	}
	if path == "verify" {
		jsonOK(w, map[string]any{"desired_hash": bundle.BundleHash, "controller_applied_hash": controllerHash, "controller_status": "applied", "verified_at": time.Now().UTC(), "assets": bundle.Assets})
		return
	}
	targets := append([]string(nil), request.NodeIDs...)
	if len(targets) == 0 {
		for _, node := range s.fleet.All() {
			if node.Status != fleet.StatusOffline {
				targets = append(targets, node.ID)
			}
		}
	}
	if len(targets) == 0 {
		jsonError(w, "no eligible online clients to retry", http.StatusConflict)
		return
	}
	proposal := map[string]any{"name": "Retry parser asset synchronization", "type": rolloutTypeParserSync, "target_nodes": targets, "payload": map[string]any{"asset_bundle": bundle, "bundle_hash": bundle.BundleHash}, "approval_required": true, "approval_reason": "Parser sync retries require an independent rollout approval."}
	name := fmt.Sprintf("parser-sync-%d", time.Now().UnixNano())
	encoded, _ := json.Marshal(proposal)
	saved, err := s.metadata.Put(r.Context(), metastore.Document{Collection: "rollout-proposals", ID: name, State: "pending_approval", Payload: encoded, CreatedBy: principal.Username})
	if err != nil {
		jsonError(w, "parser sync retry proposal could not be saved", http.StatusInternalServerError)
		return
	}
	s.auditAction(principal, "parser_sync.retry.propose", "rollout-proposals/"+saved.ID, "success", map[string]string{"bundle_hash": bundle.BundleHash})
	jsonCreated(w, map[string]any{"proposal": documentView(saved), "bundle_hash": bundle.BundleHash, "approval_gate": "pending_approval"})
}

func (s *Server) handleParserAssetTransition(w http.ResponseWriter, r *http.Request, principal *middleware.SecurityContext, collection, id, action string, doc metastore.Document) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		jsonError(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	state, payload := doc.State, map[string]any{}
	_ = json.Unmarshal(doc.Payload, &payload)
	switch action {
	case "submit":
		if state != "draft" && state != "rejected" {
			jsonError(w, "only draft or rejected assets may be submitted", http.StatusConflict)
			return
		}
		state = "in_review"
		payload["submitted_by"] = principal.Username
		payload["submitted_at"] = time.Now().UTC()
	case "approve":
		if state != "in_review" {
			jsonError(w, "asset is not awaiting review", http.StatusConflict)
			return
		}
		if doc.CreatedBy == principal.Username {
			jsonError(w, "creator cannot approve own parser asset", http.StatusConflict)
			return
		}
		state = "approved"
		payload["approved_by"] = principal.Username
		payload["approved_at"] = time.Now().UTC()
	case "publish":
		if state != "approved" {
			jsonError(w, "only approved assets can be published", http.StatusConflict)
			return
		}
		state = "active"
		payload["published_by"] = principal.Username
		payload["published_at"] = time.Now().UTC()
	default:
		jsonError(w, "unknown asset transition", http.StatusNotFound)
		return
	}
	payload["review_state"] = state
	encoded, err := json.Marshal(payload)
	if err != nil {
		jsonError(w, "asset transition failed", http.StatusInternalServerError)
		return
	}
	saved, err := s.metadata.Put(r.Context(), metastore.Document{Collection: collection, ID: id, State: state, Payload: encoded, CreatedBy: doc.CreatedBy})
	if err != nil {
		jsonError(w, "asset transition persistence failed", http.StatusInternalServerError)
		return
	}
	response := map[string]any{"asset": documentView(saved), "state": state}
	if action == "publish" && isParserAssetCollection(collection) {
		bundle, buildErr := s.buildActiveParserBundle(r.Context())
		if buildErr != nil {
			jsonError(w, "published asset bundle could not be built", http.StatusServiceUnavailable)
			return
		}
		appliedHash, materializeErr := s.materializeParserBundle(bundle)
		if materializeErr != nil {
			jsonError(w, "published asset bundle could not be applied on controller", http.StatusServiceUnavailable)
			return
		}
		now := time.Now().UTC()
		controllerState := map[string]any{"desired_hash": bundle.BundleHash, "applied_hash": appliedHash, "applied_at": now, "status": "applied", "assets": bundleRevisionMap(bundle), "auto_rollout": map[string]any{"enabled": true, "policy": "publish_to_online_clients"}}
		stateBytes, _ := json.Marshal(controllerState)
		_, _ = s.metadata.Put(r.Context(), metastore.Document{Collection: "parser-sync-state", ID: "controller", State: "applied", Payload: stateBytes, CreatedBy: principal.Username})
		rollout, rolloutErr := s.startParserBundleRollout(r.Context(), principal.Username, "Publish parser asset bundle", bundle, nil)
		if rolloutErr != nil {
			jsonError(w, "asset published and controller applied, but client rollout could not start: "+rolloutErr.Error(), http.StatusServiceUnavailable)
			return
		}
		response["bundle_hash"] = bundle.BundleHash
		response["controller_applied_hash"] = appliedHash
		response["rollout"] = rolloutSummary(rollout)
		if rollout == nil {
			response["auto_rollout_status"] = "no_online_clients"
		} else {
			response["auto_rollout_status"] = "running"
		}
	}
	actionLabel, targetPrefix := "config_asset."+action, "config-assets/"
	if isParserAssetCollection(collection) {
		actionLabel, targetPrefix = "parser_asset."+action, "parser-assets/"
	}
	s.auditAction(principal, actionLabel, targetPrefix+collection+"/"+id, "success", map[string]string{"revision": fmt.Sprint(saved.Revision), "state": state})
	jsonOK(w, response)
}

func isParserAssetCollection(collection string) bool {
	for _, candidate := range parserAssetCollections {
		if collection == candidate {
			return true
		}
	}
	return false
}

func (s *Server) listParserSync(w http.ResponseWriter, r *http.Request) {
	bundle, err := s.buildActiveParserBundle(r.Context())
	if err != nil {
		jsonError(w, "parser sync metadata unavailable", http.StatusServiceUnavailable)
		return
	}
	controllerHash := ""
	controllerStatus := "not_materialized"
	if data, err := os.ReadFile(filepath.Join(s.cfg.StateDir, "parser-assets", "bundle.json")); err == nil {
		var applied parserAssetBundle
		if json.Unmarshal(data, &applied) == nil {
			hash, _, hashErr := bundleDigest(applied)
			if hashErr == nil && hash == bundle.BundleHash {
				controllerHash = hash
				controllerStatus = "applied"
			} else {
				controllerStatus = "drifted"
			}
		}
	}
	page, _ := s.metadata.List(r.Context(), "fleet-sync", metastore.Query{Limit: 1000})
	byNode := map[string]map[string]any{}
	for _, doc := range page {
		var state map[string]any
		if json.Unmarshal(doc.Payload, &state) == nil {
			byNode[doc.ID] = state
		}
	}
	clients := []map[string]any{}
	for _, node := range s.fleet.All() {
		state := byNode[safeID(node.ID)]
		if state == nil {
			state = map[string]any{"node_id": node.ID}
		}
		applied := fmt.Sprint(state["active_asset_bundle_hash"])
		desired := bundle.BundleHash
		drift := applied != desired
		clients = append(clients, map[string]any{"node_id": node.ID, "hostname": node.Hostname, "desired_revision": bundleRevisionMap(bundle), "desired_hash": desired, "applied_revision": state["active_asset_revisions"], "applied_hash": applied, "last_seen": state["last_seen"], "status": state["asset_sync_status"], "drifted": drift, "error": state["asset_sync_error"]})
	}
	var latest *rolloutRecord
	for _, item := range s.rollouts.List() {
		if item.Type != rolloutTypeParserSync {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(item.Payload, &payload) == nil && fmt.Sprint(payload["bundle_hash"]) == bundle.BundleHash {
			latest = item
			break
		}
	}
	assetViews := []map[string]any{}
	for _, asset := range bundle.Assets {
		var controllerRevision any
		if controllerStatus == "applied" {
			controllerRevision = asset.Revision
		}
		desired := map[string]any{"revision": asset.Revision, "sha256": asset.SHA256, "state": "published"}
		controller := map[string]any{"revision": controllerRevision, "sha256": controllerHash, "status": controllerStatus}
		assetViews = append(assetViews, map[string]any{"asset_class": asset.AssetClass, "asset_id": asset.AssetID, "name": asset.Name, "desired": desired, "controller": controller, "desired_revision": asset.Revision, "desired_hash": asset.SHA256, "state": "published", "controller_applied_revision": controllerRevision, "controller_applied_hash": controllerHash, "controller_status": controllerStatus, "auto_rollout": map[string]any{"enabled": true, "status": rolloutStatus(latest)}, "rollout": rolloutSummary(latest), "clients": clients})
	}
	jsonOK(w, map[string]any{"assets": assetViews, "count": len(assetViews), "bundle": map[string]any{"revision": bundleRevisionMap(bundle), "desired_hash": bundle.BundleHash, "controller_applied_hash": controllerHash, "controller_status": controllerStatus}, "clients": clients, "rollout": rolloutSummary(latest), "automatic_rollout_policy": "publish_active_assets_to_online_clients"})
}

func bundleRevisionMap(bundle parserAssetBundle) map[string]int {
	out := map[string]int{}
	for _, asset := range bundle.Assets {
		out[asset.AssetClass+"/"+asset.AssetID] = asset.Revision
	}
	return out
}
func rolloutStatus(item *rolloutRecord) string {
	if item == nil {
		return "not_started"
	}
	return item.Status
}
func rolloutSummary(item *rolloutRecord) any {
	if item == nil {
		return map[string]any{"status": "not_started"}
	}
	return map[string]any{"id": item.ID, "status": item.Status, "total_nodes": item.TotalNodes, "done_nodes": item.DoneNodes, "failed_nodes": item.FailedNodes, "target_nodes": item.TargetNodes, "node_results": item.NodeResults}
}
