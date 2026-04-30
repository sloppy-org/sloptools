package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func cmdBrainGTD(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain gtd requires parse, validate, list, or update")
		return 2
	}
	switch args[0] {
	case "parse":
		return cmdBrainGTDParse(args[1:])
	case "validate":
		return cmdBrainGTDValidate(args[1:])
	case "list":
		return cmdBrainGTDList(args[1:])
	case "update":
		return cmdBrainGTDUpdate(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain gtd <parse|validate|list|update> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --path PATH     GTD note path")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain gtd subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainFolder(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain folder requires parse, validate, links, or audit")
		return 2
	}
	switch args[0] {
	case "parse":
		return cmdBrainFolderParse(args[1:])
	case "validate":
		return cmdBrainFolderValidate(args[1:])
	case "links":
		return cmdBrainFolderLinks(args[1:])
	case "audit":
		return cmdBrainFolderAudit(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain folder <parse|validate|links|audit> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --path PATH     folder note path")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain folder subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainGlossary(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain glossary requires parse or validate")
		return 2
	}
	switch args[0] {
	case "parse":
		return cmdBrainGlossaryParse(args[1:])
	case "validate":
		return cmdBrainGlossaryValidate(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain glossary <parse|validate> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --path PATH     glossary note path")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain glossary subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainAttention(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain attention requires parse or validate")
		return 2
	}
	switch args[0] {
	case "parse":
		return cmdBrainAttentionParse(args[1:])
	case "validate":
		return cmdBrainAttentionValidate(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain attention <parse|validate> [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --path PATH     attention note path")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain attention subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainLinks(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "brain links requires resolve")
		return 2
	}
	switch args[0] {
	case "resolve":
		return cmdBrainLinksResolve(args[1:])
	case "help", "-h", "--help":
		fmt.Println("sloptools brain links resolve [flags]")
		fmt.Println("  --config PATH   vault config path (default ~/.config/sloptools/vaults.toml)")
		fmt.Println("  --sphere NAME   vault sphere: work or private")
		fmt.Println("  --path PATH     note path")
		fmt.Println("  --link TEXT     link to resolve")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown brain links subcommand: %s\n", args[0])
		return 2
	}
}

func cmdBrainGTDParse(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain gtd parse", args)
	if status != 0 {
		return status
	}
	commitment, _, diags := braingtd.ParseCommitmentMarkdown(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"commitment":  commitment,
		"diagnostics": diags,
		"count":       len(diags),
	})
}

func cmdBrainGTDValidate(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain gtd validate", args)
	if status != 0 {
		return status
	}
	result := braingtd.ParseAndValidate(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"commitment":  result.Commitment,
		"diagnostics": result.Diagnostics,
		"count":       len(result.Diagnostics),
		"valid":       len(result.Diagnostics) == 0,
	})
}

func cmdBrainFolderParse(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain folder parse", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ParseFolderNote(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"folder":      parsed,
		"diagnostics": diags,
		"count":       len(diags),
	})
}

func cmdBrainFolderValidate(args []string) int {
	cfg, resolved, body, status := loadBrainNoteArgs("brain folder validate", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ValidateFolderNote(body, brain.LinkValidationContext{Config: cfg, Sphere: resolved.Sphere, Path: resolved.Path})
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"folder":      parsed,
		"diagnostics": diags,
		"count":       len(diags),
		"valid":       len(diags) == 0,
	})
}

func cmdBrainFolderLinks(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain folder links", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ParseFolderNote(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"links":       map[string]interface{}{"markdown_links": parsed.MarkdownLinks, "wikilinks": parsed.Wikilinks},
		"diagnostics": diags,
		"count":       len(diags),
	})
}

func cmdBrainGlossaryParse(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain glossary parse", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ParseGlossaryNote(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"glossary":    parsed,
		"diagnostics": diags,
		"count":       len(diags),
	})
}

func cmdBrainGlossaryValidate(args []string) int {
	cfg, resolved, body, status := loadBrainNoteArgs("brain glossary validate", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ValidateGlossaryNote(body, brain.LinkValidationContext{Config: cfg, Sphere: resolved.Sphere, Path: resolved.Path})
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"glossary":    parsed,
		"diagnostics": diags,
		"count":       len(diags),
		"valid":       len(diags) == 0,
	})
}

func cmdBrainAttentionParse(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain attention parse", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ParseAttentionFields(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"attention":   parsed,
		"diagnostics": diags,
		"count":       len(diags),
	})
}

func cmdBrainAttentionValidate(args []string) int {
	_, resolved, body, status := loadBrainNoteArgs("brain attention validate", args)
	if status != 0 {
		return status
	}
	parsed, diags := brain.ValidateAttentionFields(body)
	return printBrainJSON(map[string]interface{}{
		"source":      resolved,
		"attention":   parsed,
		"diagnostics": diags,
		"count":       len(diags),
		"valid":       len(diags) == 0,
	})
}

func cmdBrainLinksResolve(args []string) int {
	cfg, noteResolved, link, status := loadBrainResolveArgs("brain links resolve", args)
	if status != 0 {
		return status
	}
	resolved, err := cfg.Resolver().ResolveLink(noteResolved.Sphere, noteResolved.Path, link)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return printBrainJSON(map[string]interface{}{
		"source":   noteResolved,
		"link":     link,
		"resolved": resolved,
	})
}

func loadBrainNoteArgs(command string, args []string) (*brain.Config, brain.ResolvedPath, string, int) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "note path")
	if err := fs.Parse(args); err != nil {
		return nil, brain.ResolvedPath{}, "", 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return nil, brain.ResolvedPath{}, "", 2
	}
	if strings.TrimSpace(*path) == "" {
		fmt.Fprintln(os.Stderr, "--path is required")
		return nil, brain.ResolvedPath{}, "", 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, brain.ResolvedPath{}, "", 1
	}
	resolved, data, err := brain.ReadNoteFile(cfg, brain.Sphere(*sphere), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, brain.ResolvedPath{}, "", 1
	}
	return cfg, resolved, string(data), 0
}

func loadBrainResolveArgs(command string, args []string) (*brain.Config, brain.ResolvedPath, string, int) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := fs.String("config", "", "vault config path")
	sphere := fs.String("sphere", "", "vault sphere")
	path := fs.String("path", "", "note path")
	link := fs.String("link", "", "link to resolve")
	if err := fs.Parse(args); err != nil {
		return nil, brain.ResolvedPath{}, "", 2
	}
	if strings.TrimSpace(*sphere) == "" {
		fmt.Fprintln(os.Stderr, "--sphere is required")
		return nil, brain.ResolvedPath{}, "", 2
	}
	if strings.TrimSpace(*path) == "" {
		fmt.Fprintln(os.Stderr, "--path is required")
		return nil, brain.ResolvedPath{}, "", 2
	}
	if strings.TrimSpace(*link) == "" {
		fmt.Fprintln(os.Stderr, "--link is required")
		return nil, brain.ResolvedPath{}, "", 2
	}
	cfg, err := brain.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, brain.ResolvedPath{}, "", 1
	}
	resolved, _, err := brain.ReadNoteFile(cfg, brain.Sphere(*sphere), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, brain.ResolvedPath{}, "", 1
	}
	return cfg, resolved, *link, 0
}
