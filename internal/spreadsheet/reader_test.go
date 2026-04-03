package spreadsheet

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReaderOpenAndReadSheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workbook.xlsx")
	wb := excelize.NewFile()
	wb.SetSheetName("Sheet1", "Tasks")
	if err := wb.SetCellValue("Tasks", "A1", "Title"); err != nil {
		t.Fatalf("SetCellValue(A1) error: %v", err)
	}
	if err := wb.SetCellValue("Tasks", "B1", "Owner"); err != nil {
		t.Fatalf("SetCellValue(B1) error: %v", err)
	}
	if err := wb.SetCellValue("Tasks", "A2", "Port providers"); err != nil {
		t.Fatalf("SetCellValue(A2) error: %v", err)
	}
	if err := wb.SetCellValue("Tasks", "B2", "Slopshell"); err != nil {
		t.Fatalf("SetCellValue(B2) error: %v", err)
	}
	if err := wb.SaveAs(path); err != nil {
		t.Fatalf("SaveAs() error: %v", err)
	}
	if err := wb.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer reader.Close()

	sheets := reader.SheetNames()
	if len(sheets) != 1 || sheets[0] != "Tasks" {
		t.Fatalf("SheetNames() = %#v", sheets)
	}

	rows, err := reader.GetSheetAsDict("Tasks", 0, 10)
	if err != nil {
		t.Fatalf("GetSheetAsDict() error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["Title"] != "Port providers" || rows[0]["Owner"] != "Slopshell" {
		t.Fatalf("rows[0] = %#v", rows[0])
	}
}
