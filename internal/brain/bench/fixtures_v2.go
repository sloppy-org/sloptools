package bench

import "strings"

// V2TriageFixtures covers the four canonical decision classes of the
// triage stage: promote, maybe, reject-textbook, reject-duplicate. The
// vault context is pinned to the Plasma Group at TU Graz to mirror the
// real brain.
func V2TriageFixtures() []Fixture {
	return []Fixture{
		{
			ID: "fx-tri-promote-person",
			Packet: strings.Join([]string{
				"Candidate entity: a person named **Sebastian Riepl**.",
				"",
				"Evidence in the vault:",
				"- Two meeting notes in 2026-04 wikilink `[[people/sebastian-riepl]]` (note does not exist yet).",
				"- A commitment in `brain/commitments/sebastian-riepl-phd.md` references the candidate.",
				"- Mail thread metadata shows ten messages between Christopher Albert and the candidate over the past three months.",
				"",
				"External signal: the candidate's TU Graz/ViF affiliation is mentioned in one meeting note.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "promote",
				"rejection_class": "",
			},
		},
		{
			ID: "fx-tri-reject-textbook",
			Packet: strings.Join([]string{
				"Candidate entity: a topic note titled **Boltzmann equation**.",
				"",
				"Evidence in the vault:",
				"- One mention in a topics-folder draft, no wikilink to people/, projects/, institutions/, commitments/, or folders/plasma/.",
				"- Wikipedia has a comprehensive article on the Boltzmann equation.",
				"- The Plasma Group does not maintain a project named after this topic.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "reject",
				"rejection_class": "textbook",
			},
		},
		{
			ID: "fx-tri-reject-duplicate",
			Packet: strings.Join([]string{
				"Candidate entity: a project note titled **NEO-RT transport code**.",
				"",
				"Evidence in the vault:",
				"- An existing canonical note `brain/projects/NEO-RT.md` already covers the same topic with rich locally-specific content (commits, contributors, reviewers).",
				"- The candidate is the same physical project under a slightly different label.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "reject",
				"rejection_class": "duplicate",
			},
		},
		{
			ID: "fx-tri-adversarial-neort-vs-neutron",
			Packet: strings.Join([]string{
				"Candidate entity: a project note titled **NEO-RT** (capital letters, hyphenated).",
				"",
				"Evidence in the vault:",
				"- Five recent meeting notes wikilink `[[projects/NEO-RT]]` (note already exists).",
				"- Repository `code/sloppy/NEO-RT` is active; commits in the past two weeks by Christopher Albert and collaborators.",
				"- 'Neutron transport' is a separate textbook concept; do not conflate.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "reject",
				"rejection_class": "duplicate",
			},
		},
		{
			ID: "fx-tri-adversarial-simple-method-vs-simple-project",
			Packet: strings.Join([]string{
				"Candidate entity: a topic note titled **SIMPLE method**.",
				"",
				"Evidence in the vault:",
				"- The token 'SIMPLE' in the Plasma Group always means the SIMPLE orbit code (`brain/projects/SIMPLE.md`).",
				"- The candidate's body describes Patankar's Semi-Implicit Method for Pressure-Linked Equations from Computational Fluid Dynamics, which has no anchor to Christopher Albert or his Plasma Group.",
				"- No local references to SIMPLE-project commits, contributors, or runs.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "reject",
				"rejection_class": "textbook",
			},
		},
		{
			ID: "fx-tri-adversarial-kilca-promote",
			Packet: strings.Join([]string{
				"Candidate entity: a project note titled **KiLCA** (Kinetic Linear Code for Alfvén waves).",
				"",
				"Evidence in the vault:",
				"- Two folder notes under `brain/folders/plasma/CODES/KiLCA/` reference the candidate.",
				"- `brain/people/martin-heyn.md` is the long-term maintainer of KiLCA in the Plasma Group at TU Graz.",
				"- A meeting note from 2026-04 schedules a KiLCA roadmap review.",
				"",
				"Note: 'KiLCA' looks like an acronym but is a canonical Plasma Group code, not a generic killer-app concept.",
				"",
				"Decide promote / maybe / reject. Return the JSON verdict.",
			}, "\n"),
			Expected: map[string]string{
				"verdict":         "promote",
				"rejection_class": "",
			},
		},
	}
}

// V2SleepJudgeFixtures: small synthetic packets with explicit required
// and forbidden sections.
func V2SleepJudgeFixtures() []Fixture {
	return []Fixture{
		{
			ID: "fx-sj-default",
			Packet: strings.Join([]string{
				"# Sleep packet 2026-05-07",
				"",
				"## Prune candidates",
				"- brain/topics/leftover-1.md (no inbound links since 2025-12)",
				"",
				"## NREM consolidation",
				"- brain/people/jane-doe.md → brain/people/jane-doe.md (status update from meeting 2026-05-06)",
				"",
				"## REM dream candidates",
				"- brain/projects/NEO-RT.md (3 commits this week)",
				"",
				"## Folder coverage",
				"- 1 new folder under plasma/CODES/",
				"",
				"## Recent paths",
				"- brain/people/jane-doe.md",
				"",
				"Apply only the changes the packet authorises. Output the persisted Markdown report.",
			}, "\n"),
			Expected: map[string]string{
				"expected_sections":  "## Prune candidates,## NREM consolidation,## REM dream candidates,## Folder coverage",
				"forbidden_sections": "## TODO,## Action items,## Random thoughts",
			},
		},
		{
			ID: "fx-sj-empty-day",
			Packet: strings.Join([]string{
				"# Sleep packet 2026-05-07",
				"",
				"## Prune candidates",
				"- (none)",
				"",
				"## NREM consolidation",
				"- (none)",
				"",
				"## REM dream candidates",
				"- (none)",
				"",
				"## Folder coverage",
				"- (no changes)",
				"",
				"## Recent paths",
				"- (none)",
				"",
				"Apply only the changes the packet authorises. Output the persisted Markdown report.",
			}, "\n"),
			Expected: map[string]string{
				"expected_sections":  "## Prune candidates,## NREM consolidation,## REM dream candidates,## Folder coverage",
				"forbidden_sections": "## TODO,## Random thoughts",
			},
		},
	}
}

// V2ScoutFixtures cover one entity per canonical-entity category. The
// packet pretends an MCP-derived evidence set is available; the model
// is graded only on output structure and source citation.
func V2ScoutFixtures() []Fixture {
	return []Fixture{
		{
			ID: "fx-sc-person",
			Packet: strings.Join([]string{
				"# Scout verification packet",
				"",
				"Entity path: `people/winfried-kernbichler.md`",
				"Title: Winfried Kernbichler",
				"Cadence: monthly",
				"Last seen: 2026-04-01",
				"",
				"## Current note body",
				"```markdown",
				"---",
				"role: emeritus",
				"affiliation: TU Graz / Plasma Group",
				"cadence: monthly",
				"---",
				"",
				"# Winfried Kernbichler",
				"",
				"Long-time mentor and collaborator. Co-author on neoclassical transport papers.",
				"```",
				"",
				"## Your task",
				"Verify this entity and write the evidence report.",
			}, "\n"),
			Expected: map[string]string{},
		},
		{
			ID: "fx-sc-project",
			Packet: strings.Join([]string{
				"# Scout verification packet",
				"",
				"Entity path: `projects/EUROfusion-WPPRD.md`",
				"Title: EUROfusion WPPRD",
				"Cadence: quarterly",
				"Last seen: 2026-02-10",
				"",
				"## Current note body",
				"```markdown",
				"---",
				"deliverable: D-1515000028",
				"cadence: quarterly",
				"---",
				"",
				"# EUROfusion WPPRD",
				"",
				"Plasma Reactor Design work package. Christopher Albert is task lead.",
				"```",
				"",
				"## Your task",
				"Verify and write the evidence report.",
			}, "\n"),
			Expected: map[string]string{},
		},
	}
}

// V2CompressFixtures cover textbook-prose collapse with retained local
// anchors.
func V2CompressFixtures() []Fixture {
	return []Fixture{
		{
			ID: "fx-cp-banana",
			Packet: strings.Join([]string{
				"---",
				"title: Banana regime",
				"---",
				"",
				"# Banana regime",
				"",
				"The banana regime arises in low-collisionality plasmas where trapped particles complete a banana orbit before being scattered.",
				"The collisionality parameter ν* satisfies ν* < 1 in this regime, and the resulting transport coefficients deviate from the Pfirsch-Schlüter prediction.",
				"For an axisymmetric tokamak, the bounce-averaged drift kinetic equation predicts a parallel current proportional to the inverse aspect ratio.",
				"",
				"In our work, the [[projects/NEO-RT]] code computes neoclassical transport in the banana regime; see [[topics/1-nu-transport]] for the regime ν → 0.",
				"",
				"## Notes",
				"- Cross-checked against Goedbloed–Poedts ch. 4 in 2025.",
				"- See [[people/winfried-kernbichler]] for the historical group context.",
			}, "\n"),
			Expected: map[string]string{
				"expected_local_anchors": "[[projects/NEO-RT]],[[topics/1-nu-transport]],[[people/winfried-kernbichler]]",
			},
		},
		{
			ID: "fx-cp-adversarial-neutron-transport",
			Packet: strings.Join([]string{
				"---",
				"title: Neutron transport",
				"---",
				"",
				"# Neutron transport",
				"",
				"Neutron transport theory describes the migration of free neutrons in matter; the linear Boltzmann equation governs the population in phase space.",
				"Standard reference: Bell and Glasstone, *Nuclear Reactor Theory*. Wikipedia covers the general formalism in detail.",
				"",
				"In our group, we use the [[projects/NEO-RT]] code with a banana-regime closure rather than full neutron transport; the historical context is in [[folders/plasma/CODES/NEO-RT]].",
				"[[people/winfried-kernbichler]] introduced the closure scheme to the group.",
				"",
				"## Notes",
				"- Cross-checked vs Bell-Glasstone in 2024.",
			}, "\n"),
			Expected: map[string]string{
				"expected_local_anchors": "[[projects/NEO-RT]],[[folders/plasma/CODES/NEO-RT]],[[people/winfried-kernbichler]]",
			},
		},
		{
			ID: "fx-cp-mhd",
			Packet: strings.Join([]string{
				"---",
				"title: MHD",
				"---",
				"",
				"# MHD",
				"",
				"Magnetohydrodynamics couples Maxwell's equations to the fluid equations under the assumption that the displacement current is negligible.",
				"The ideal-MHD limit assumes infinite conductivity; resistive MHD adds Ohmic dissipation. The Grad-Shafranov equation describes axisymmetric equilibrium.",
				"Standard textbooks (Freidberg; Goedbloed-Poedts) cover the wave families: Alfvén, fast magnetosonic, slow magnetosonic.",
				"",
				"In the Plasma Group at TU Graz, [[projects/KiLCA]] uses ideal MHD as a starting point for kinetic edge corrections; see [[folders/plasma/CODES/KiLCA]].",
				"",
				"## Notes",
				"- [[people/martin-heyn]] holds the long view of the local MHD modelling history.",
			}, "\n"),
			Expected: map[string]string{
				"expected_local_anchors": "[[projects/KiLCA]],[[folders/plasma/CODES/KiLCA]],[[people/martin-heyn]]",
			},
		},
	}
}
