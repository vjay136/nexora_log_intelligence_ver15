-- V7 keeps controller authentication authoritative while mirroring only
-- non-secret account metadata into the generic Supabase document store.
begin;

do $$
begin
  if not exists (
    select 1
      from pg_constraint c
      join pg_class t on t.oid = c.conrelid
      join pg_namespace n on n.oid = t.relnamespace
     where n.nspname = 'public'
       and t.relname = 'nli_documents'
       and c.conname = 'nli_documents_users_no_credentials'
  ) then
    alter table public.nli_documents
      add constraint nli_documents_users_no_credentials check (
        collection <> 'users' or not (payload ?| array[
          'password_hash', 'passwordhash', 'salt', 'jwt_secret',
          'access_token', 'refresh_token'
        ]::text[])
      ) not valid;
  end if;
end;
$$;

alter table public.nli_documents
  validate constraint nli_documents_users_no_credentials;

comment on constraint nli_documents_users_no_credentials on public.nli_documents is
  'Controller user metadata must never contain password hashes, salts, or authentication tokens.';

commit;
