package spreadsheet

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Reader provides spreadsheet reading functionality for Excel files.
// Note: ODS format is not supported in Go. Use XLSX instead.
type Reader struct {
	file     *excelize.File
	filepath string
}

// Open opens a spreadsheet file for reading.
// Supported formats: .xlsx, .xlsm, .xltm, .xltx
func Open(path string) (*Reader, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".xlsx" && ext != ".xlsm" && ext != ".xltm" && ext != ".xltx" {
		return nil, fmt.Errorf("unsupported file format: %s (only Excel formats supported)", ext)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open spreadsheet: %w", err)
	}

	return &Reader{
		file:     f,
		filepath: path,
	}, nil
}

// SheetNames returns the list of sheet names in the workbook.
func (r *Reader) SheetNames() []string {
	return r.file.GetSheetList()
}

// GetSheetData extracts data from a specific sheet as a 2D slice.
// If maxRows is 0, all rows are returned.
func (r *Reader) GetSheetData(sheetName string, maxRows int) ([][]string, error) {
	rows, err := r.file.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows from sheet %q: %w", sheetName, err)
	}

	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	return rows, nil
}

// GetSheetAsDict extracts sheet data as a list of maps using the header row as keys.
// headerRow is 0-indexed.
func (r *Reader) GetSheetAsDict(sheetName string, headerRow, maxRows int) ([]map[string]string, error) {
	rows, err := r.file.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows from sheet %q: %w", sheetName, err)
	}

	if len(rows) <= headerRow {
		return nil, nil
	}

	headers := rows[headerRow]
	for i, h := range headers {
		if h == "" {
			headers[i] = fmt.Sprintf("col_%d", i)
		}
	}

	var result []map[string]string
	dataRows := rows[headerRow+1:]

	if maxRows > 0 && len(dataRows) > maxRows {
		dataRows = dataRows[:maxRows]
	}

	for _, row := range dataRows {
		rowMap := make(map[string]string)
		for i, header := range headers {
			if i < len(row) {
				rowMap[header] = row[i]
			} else {
				rowMap[header] = ""
			}
		}
		result = append(result, rowMap)
	}

	return result, nil
}

// GetCellValue returns the value of a specific cell.
func (r *Reader) GetCellValue(sheetName, cell string) (string, error) {
	return r.file.GetCellValue(sheetName, cell)
}

// SetCellValue sets the value of a specific cell.
func (r *Reader) SetCellValue(sheetName, cell string, value interface{}) error {
	return r.file.SetCellValue(sheetName, cell, value)
}

// Save saves changes to the spreadsheet.
func (r *Reader) Save() error {
	return r.file.Save()
}

// SaveAs saves the spreadsheet to a new file.
func (r *Reader) SaveAs(path string) error {
	return r.file.SaveAs(path)
}

// Close closes the spreadsheet file.
func (r *Reader) Close() error {
	return r.file.Close()
}
