package groupware

// Supports is a tiny capability probe used by MCP handlers to ask whether a
// provider satisfies a more specific interface, e.g. contacts/calendar/task
// extensions beyond the core mail contract.
func Supports[T any](p any) (T, bool) {
	v, ok := p.(T)
	return v, ok
}
