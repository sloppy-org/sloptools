package meetings

import (
	"strings"
)

// AssignIDs returns the source with stable `<!-- gtd:<id> -->` comments
// stamped onto each Action Checklist `- [ ]` / `- [x]` line that does not
// already carry one. The returned task list reflects the post-stamp state
// (every task carries an ID). When no changes are required the source is
// returned unchanged and changed=false.
func AssignIDs(slug, src string) (string, []Task, bool) {
	scope := scanScope{}
	lines := strings.Split(src, "\n")
	changed := false
	tasks := make([]Task, 0)
	for index, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if heading, level := parseHeading(line); level > 0 {
			scope.update(level, heading)
			continue
		}
		if !scope.inActionChecklist {
			continue
		}
		task, ok := parseChecklistLine(scope.person, line, index+1)
		if !ok {
			continue
		}
		if task.ID == "" {
			task.ID = ComputeID(slug, task.Person, task.Text)
			lines[index] = stampID(raw, task.ID)
			changed = true
		}
		tasks = append(tasks, task)
	}
	if !changed {
		return src, tasks, false
	}
	return strings.Join(lines, "\n"), tasks, true
}

// stampID appends a `<!-- gtd:<id> -->` comment to a checklist line,
// preserving the trailing carriage-return / newline sequence the caller
// split on so round-trip rendering is byte-stable for non-task lines.
func stampID(line, id string) string {
	suffix := ""
	body := line
	if strings.HasSuffix(body, "\r") {
		suffix = "\r"
		body = strings.TrimSuffix(body, "\r")
	}
	body = strings.TrimRight(body, " \t")
	return body + " " + FormatComment(id) + suffix
}
