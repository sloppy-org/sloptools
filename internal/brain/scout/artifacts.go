package scout

import "github.com/sloppy-org/sloptools/internal/brain/audit"

// stageRecord and auditFile are package-private aliases over the shared
// audit package types so the rest of scout (runner, escalate, resolve)
// keeps its current vocabulary while sleep / future stages reuse the
// same JSON schema and on-disk layout. Extracted into the shared
// package so the bulk → resolve → escalate sidecar pattern can be
// applied to other brain-night stages without copy-paste.
type stageRecord = audit.StageRecord

type auditFile = audit.File

// writeStageArtifact and writeAuditFile are thin re-exports so the
// existing call sites in scout do not need to import internal/brain/
// audit directly.
func writeStageArtifact(reportPath, suffix, raw, cleaned string) (string, string, error) {
	return audit.WriteStageArtifact(reportPath, suffix, raw, cleaned)
}

func writeAuditFile(reportPath string, file auditFile) error {
	return audit.WriteFile(reportPath, file)
}
