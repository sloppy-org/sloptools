package bench

import "strings"

// V1FolderNoteFixtures is the v1 fixture set for the folder-note
// authoring task: synthetic, anchored to known local entities so the
// rubric can check anchor presence without leaking real brain content.
//
// Three fixtures cover (a) a project folder, (b) a teaching folder, (c)
// a meeting folder. Each fixture's Expected.expected_anchor is a
// project, course, or person name the model must mention.
func V1FolderNoteFixtures() []Fixture {
	return []Fixture{
		{
			ID: "fx-neort",
			Packet: strings.Join([]string{
				"Write a strict folder note for the source folder `plasma/CODES/NEO-RT`.",
				"",
				"Direct files in the folder:",
				"- README.md",
				"- 01-overview.md (description of the NEO-RT transport code, links to projects/NEO-RT)",
				"- 02-build.md (CMake build instructions, dependencies on LAPACK)",
				"- 03-tests.md (regression tests, references to projects/NEO-RT)",
				"",
				"Direct child folders:",
				"- examples/ (containing fixture inputs)",
				"- doc/ (Sphinx documentation)",
				"",
				"Existing related notes:",
				"- brain/projects/NEO-RT.md",
				"- brain/topics/1-nu-transport.md",
				"",
				"Output the strict folder-note Markdown body and only that body.",
			}, "\n"),
			Expected: map[string]string{
				"expected_anchor": "projects/NEO-RT",
			},
		},
		{
			ID: "fx-wsd",
			Packet: strings.Join([]string{
				"Write a strict folder note for the source folder `lv/wsd`.",
				"",
				"Direct files in the folder:",
				"- README.md",
				"- syllabus.md (Wahrscheinlichkeit und Statistik UE syllabus)",
				"- exam-2025.md (problem set)",
				"- grading.md (rubric)",
				"",
				"Direct child folders:",
				"- skript/ (lecture notes)",
				"- ue/ (problem sets)",
				"",
				"Existing related notes:",
				"- brain/people/<student-A>.md",
				"- brain/people/<student-B>.md",
				"",
				"Output the strict folder-note Markdown body and only that body.",
			}, "\n"),
			Expected: map[string]string{
				"expected_anchor": "Wahrscheinlichkeit",
			},
		},
		{
			ID: "fx-eufus",
			Packet: strings.Join([]string{
				"Write a strict folder note for the source folder `plasma/DOCUMENTS/EURATOM/2026`.",
				"",
				"Direct files in the folder:",
				"- D-1515000028-deliverable-draft.md",
				"- WPPRD-meeting-2026-03-21.md",
				"- annual-report-outline.md",
				"",
				"Direct child folders:",
				"- attachments/",
				"",
				"Existing related notes:",
				"- brain/projects/EUROfusion-WPPRD.md",
				"- brain/institutions/EUROfusion.md",
				"",
				"Output the strict folder-note Markdown body and only that body.",
			}, "\n"),
			Expected: map[string]string{
				"expected_anchor": "EUROfusion",
			},
		},
		// fx-neort-with-glossary repeats fx-neort but injects the
		// glossary stub the production packet builder would emit. The
		// 2026-05-07 bench had qwen score 0.25 on fx-neort because it
		// invented "neutrino transport" for `1-nu-transport`. With the
		// stub in the packet the judge expects no invented_facts. This
		// is the bench-side validation that the glossary auto-injection
		// flow lifts qwen out of the fabrication trap; the production
		// packet builder calls glossary.FormatPacketSection so the same
		// `## Glossary context` block lands in real packets.
		{
			ID: "fx-neort-with-glossary",
			Packet: strings.Join([]string{
				"Write a strict folder note for the source folder `plasma/CODES/NEO-RT`.",
				"",
				"Direct files in the folder:",
				"- README.md",
				"- 01-overview.md (description of the NEO-RT transport code, links to projects/NEO-RT)",
				"- 02-build.md (CMake build instructions, dependencies on LAPACK)",
				"- 03-tests.md (regression tests, references to projects/NEO-RT)",
				"",
				"Direct child folders:",
				"- examples/ (containing fixture inputs)",
				"- doc/ (Sphinx documentation)",
				"",
				"Existing related notes:",
				"- brain/projects/NEO-RT.md",
				"- brain/topics/1-nu-transport.md",
				"",
				"## Glossary context",
				"",
				"Local-vocabulary terms recognised in this entity. Use these definitions verbatim — do not reinterpret common acronyms by general knowledge. Follow the canonical_topic link for fuller context.",
				"",
				"- **1/ν transport**: Neoclassical-transport regime in stellarator plasmas where the radial particle and energy fluxes scale as 1/ν with the collision frequency ν. Distinct from neutrino or neutron transport (different physics entirely). (path: `brain/glossary/1-nu-transport.md`; canonical: [[topics/1-nu-transport]]; do not confuse with: neutrino transport, neutron transport)",
				"",
				"Output the strict folder-note Markdown body and only that body.",
			}, "\n"),
			Expected: map[string]string{
				"expected_anchor": "projects/NEO-RT",
			},
		},
	}
}
