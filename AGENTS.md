# Repository Map

## Code Map

- `cmd/sloptools`: CLI entrypoint and command wiring.
- `internal/appserver`: app-session client, approval, input, and session state helpers.
- `internal/bear`: Bear notes adapter.
- `internal/calendar`: calendar provider resolution plus Google Calendar and ICS account support.
- `internal/canvas`: canvas event and adapter types.
- `internal/contacts`: contact provider adapters.
- `internal/document`: document build, figure, Pandoc, and SyncTeX helpers.
- `internal/email`: common email provider interfaces plus Gmail, IMAP, Exchange, draft, reply, and server-filter behavior.
- `internal/email/imaptest`: in-process IMAP test server.
- `internal/evernote`: Evernote API client, ENML helpers, and types.
- `internal/ews`: Exchange Web Services client.
- `internal/googleauth`: OAuth session helpers.
- `internal/groupware`: external account registry that builds mail, calendar, task, and notes providers.
- `internal/ics`: ICS calendar client.
- `internal/licensing`: license-policy tests and repository-wide size-limit enforcement.
- `internal/llmcache`: local cache and cache-key helpers for LLM calls.
- `internal/mailboxsettings`: mailbox setting providers (Gmail and EWS) covering out-of-office state and delegation/shared-mailbox listing.
- `internal/mailtriage`: mail triage models, distillation, hybrid routing, and LLM integration.
- `internal/mcp`: MCP server, tool registration, domain/mail/calendar/handoff handlers, and compose/flag flows.
- `internal/modelprofile`: model-profile definitions and validation.
- `internal/protocol`: protocol bootstrap helpers.
- `internal/providerdata`: provider-neutral data models.
- `internal/serve`: app runtime and static file serving.
- `internal/spreadsheet`: spreadsheet reader support.
- `internal/store`: SQLite-backed domain store public surface, split into declaration-packed files.
- `internal/store/providerkind`: provider classification and display-name helpers.
- `internal/store/records`: store-backed record handlers for push registrations and mail logs/reviews.
- `internal/store/storetest`: external tests for store behavior and migrations.
- `internal/surface`: MCP tool surface definitions.
- `internal/sync`: synchronization flow and store sink.
- `internal/tasks`: task provider interfaces and Google Tasks adapter.
- `internal/todoist`: Todoist API client and types.
- `internal/update`: binary update helpers.
- `internal/zotero`: Zotero API/local-library reader, citation, attachment, and type helpers.

## Docs Map

- `README.md`: user-facing project overview and license/disclaimer pointer.
- `AGENTS.md`: contributor and agent navigation map for this repository.
- `.github/workflows/test.yml`: canonical CI commands: `gofmt -s -l .`, `go vet ./...`, and `go test ./... -race`.
- `deploy/systemd/user/sloptools.service`: systemd user deployment unit.
- `deploy/launchd/io.sloptools.mcp.plist`: macOS launchd deployment unit.
- `LICENSE`: MIT license text.

## Local Rules

- Keep every file under 500 lines and every folder at 20 direct files or fewer.
- Run `go test ./...` after structural refactors; CI also runs gofmt, go vet, and race-enabled tests.
- Use the package map above before adding new files so related behavior stays in the smallest coherent package.
