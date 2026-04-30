package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func cmdBrainVault(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain vault requires list or validate")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdBrainVaultList(args[1:])
	case "validate":
		return cmdBrainVaultValidate(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain vault <list|validate> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain vault subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainVaultValidate(args []string) int {
	fs := flag.NewFlagSet("brain vault validate", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	vault, ok := cfg.Vault(brain.Sphere(*sphere))
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown vault %q\n", *sphere)
		return 1
	}
	var notes []map[string]interface{}
	issues := 0
	err = brain.WalkVaultNotes(cfg, brain.Sphere(*sphere), func(snapshot brain.NoteSnapshot) error {
		entry, err := brainCLIInspectNoteContent(cfg, snapshot, true)
		if err != nil {
			return err
		}
		notes = append(notes, map[string]interface{}{
			"source":      entry["source"],
			"kind":        entry["kind"],
			"diagnostics": entry["diagnostics"],
			"count":       entry["count"],
			"valid":       entry["valid"],
		})
		if count, ok := entry["count"].(int); ok {
			issues += count
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"source": brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot()},
		"notes":  notes,
		"count":  len(notes),
		"issues": issues,
		"valid":  issues == 0,
	})
}

func cmdBrainVaultList(args []string) int {
	fs := flag.NewFlagSet("brain vault list", flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"vaults": brain.ListVaults(cfg),
		"count":  len(cfg.Vaults),
	})
}

func brainCLIInspectNoteContent(cfg *brain.Config, snapshot brain.NoteSnapshot, validate bool) (map[string]interface{}, error) {
	kind, note, parseDiags := brainCLINoteKind(snapshot.Body)
	result := map[string]interface{}{
		"source": snapshot.Source,
		"kind":   kind,
	}
	switch kind {
	case "commitment":
		if validate {
			parsed := braingtd.ParseAndValidate(snapshot.Body)
			result["parsed"] = parsed.Commitment
			result["commitment"] = parsed.Commitment
			result["diagnostics"] = parsed.Diagnostics
			result["count"] = len(parsed.Diagnostics)
			result["valid"] = len(parsed.Diagnostics) == 0
			return result, nil
		}
		commitment, _, diags := braingtd.ParseCommitmentMarkdown(snapshot.Body)
		result["parsed"] = commitment
		result["commitment"] = commitment
		result["diagnostics"] = diags
		result["count"] = len(diags)
		return result, nil
	case "folder":
		var parsed brain.FolderNote
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateFolderNote(snapshot.Body, brain.LinkValidationContext{Config: cfg, Sphere: snapshot.Source.Sphere, Path: snapshot.Source.Path})
		} else {
			parsed, diags = brain.ParseFolderNote(snapshot.Body)
		}
		result["parsed"] = parsed
		result["folder"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	case "glossary":
		var parsed brain.GlossaryNote
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateGlossaryNote(snapshot.Body, brain.LinkValidationContext{Config: cfg, Sphere: snapshot.Source.Sphere, Path: snapshot.Source.Path})
		} else {
			parsed, diags = brain.ParseGlossaryNote(snapshot.Body)
		}
		result["parsed"] = parsed
		result["glossary"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	case "attention", "human", "project", "topic", "institution":
		var parsed brain.AttentionFields
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateAttentionFields(snapshot.Body)
		} else {
			parsed, diags = brain.ParseAttentionFields(snapshot.Body)
		}
		result["parsed"] = parsed
		result["attention"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	default:
		result["parsed"] = map[string]interface{}{"sections": note.Sections()}
		result["markdown"] = map[string]interface{}{"sections": note.Sections()}
		result["diagnostics"] = parseDiags
		result["count"] = len(parseDiags)
		result["valid"] = len(parseDiags) == 0
		return result, nil
	}
}

func brainCLINoteKind(body string) (string, *brain.MarkdownNote, []brain.MarkdownDiagnostic) {
	note, diags := brain.ParseMarkdownNote(body, brain.MarkdownParseOptions{})
	kind := ""
	if note != nil {
		if node, ok := note.FrontMatterField("kind"); ok {
			kind = strings.ToLower(strings.TrimSpace(node.Value))
			if kind == "gtd" {
				kind = "commitment"
			}
		}
	}
	return kind, note, diags
}
