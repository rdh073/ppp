package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type squaresToTapsParams struct {
	Squares    string `json:"squares"`    // JSON-array string, e.g. "[2,3,4]"
	Cols       string `json:"cols"`       // grid columns (and rows); default "4"
	GridBounds string `json:"gridBounds"` // JSON-array string "[left,top,right,bottom]"
}

type squaresToTapsResult struct {
	Tap0  string `json:"tap0"`
	Tap1  string `json:"tap1"`
	Tap2  string `json:"tap2"`
	Tap3  string `json:"tap3"`
	Tap4  string `json:"tap4"`
	Tap5  string `json:"tap5"`
	Tap6  string `json:"tap6"`
	Tap7  string `json:"tap7"`
	Tap8  string `json:"tap8"`
	Tap9  string `json:"tap9"`
	Tap10 string `json:"tap10"`
	Tap11 string `json:"tap11"`
	Tap12 string `json:"tap12"`
	Tap13 string `json:"tap13"`
	Tap14 string `json:"tap14"`
	Tap15 string `json:"tap15"`
	Count string `json:"count"`
}

func squaresToTapsTool() ToolDefinition {
	return ToolDefinition{
		Manifest: ToolManifest{
			Name: "captcha.squares_to_taps",
			Description: "Translates 1-indexed grid square numbers into absolute screen tap coordinates. " +
				"Assumes a square grid (rows = cols). Returns up to 16 tap slots (tap0..tap15); unused slots are empty strings.",
			Deterministic: true,
			Timeout:       250 * time.Millisecond,
			InputSchema: rawSchema(`{
				"type":"object",
				"required":["squares","gridBounds"],
				"properties":{
					"squares":{"type":"string","minLength":2},
					"cols":{"type":"string"},
					"gridBounds":{"type":"string","minLength":2}
				}
			}`),
			OutputSchema: rawSchema(`{
				"type":"object",
				"required":["tap0","tap1","tap2","tap3","tap4","tap5","tap6","tap7","tap8","tap9","tap10","tap11","tap12","tap13","tap14","tap15","count"],
				"properties":{
					"tap0":{"type":"string"},"tap1":{"type":"string"},"tap2":{"type":"string"},
					"tap3":{"type":"string"},"tap4":{"type":"string"},"tap5":{"type":"string"},
					"tap6":{"type":"string"},"tap7":{"type":"string"},"tap8":{"type":"string"},
					"tap9":{"type":"string"},"tap10":{"type":"string"},"tap11":{"type":"string"},
					"tap12":{"type":"string"},"tap13":{"type":"string"},"tap14":{"type":"string"},
					"tap15":{"type":"string"},"count":{"type":"string"}
				}
			}`),
		},
		ValidateParams: validateSquaresToTapsParams,
		ValidateResult: validateSquaresToTapsResult,
		Handler: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			squares, bounds, cols, err := parseSquaresToTapsParams(raw)
			if err != nil {
				return nil, err
			}
			rows := cols
			cellW := float64(bounds[2]-bounds[0]) / float64(cols)
			cellH := float64(bounds[3]-bounds[1]) / float64(rows)

			var taps [16]string
			count := 0
			for i, sq := range squares {
				if i >= 16 {
					break
				}
				col := (sq - 1) % cols
				row := (sq - 1) / cols
				cx := float64(bounds[0]) + float64(col)*cellW + cellW/2.0
				cy := float64(bounds[1]) + float64(row)*cellH + cellH/2.0
				taps[i] = fmt.Sprintf("%d,%d", int(cx), int(cy))
				count++
			}

			return marshalResult(squaresToTapsResult{
				Tap0: taps[0], Tap1: taps[1], Tap2: taps[2],
				Tap3: taps[3], Tap4: taps[4], Tap5: taps[5],
				Tap6: taps[6], Tap7: taps[7], Tap8: taps[8],
				Tap9: taps[9], Tap10: taps[10], Tap11: taps[11],
				Tap12: taps[12], Tap13: taps[13], Tap14: taps[14],
				Tap15: taps[15],
				Count: strconv.Itoa(count),
			})
		},
	}
}

func validateSquaresToTapsParams(raw json.RawMessage) error {
	_, _, _, err := parseSquaresToTapsParams(raw)
	return err
}

func parseSquaresToTapsParams(raw json.RawMessage) (squares []int, bounds [4]int, cols int, err error) {
	var p squaresToTapsParams
	if len(raw) == 0 {
		return nil, bounds, 0, fmt.Errorf("squares and gridBounds are required")
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, bounds, 0, fmt.Errorf("decode params: %w", err)
	}

	if strings.TrimSpace(p.Squares) == "" {
		return nil, bounds, 0, fmt.Errorf("squares is required")
	}
	if err = json.Unmarshal([]byte(p.Squares), &squares); err != nil {
		return nil, bounds, 0, fmt.Errorf("squares must be a JSON array of integers: %w", err)
	}
	for _, sq := range squares {
		if sq < 1 {
			return nil, bounds, 0, fmt.Errorf("square numbers must be >= 1, got %d", sq)
		}
	}

	cols = 4
	if strings.TrimSpace(p.Cols) != "" {
		cols, err = strconv.Atoi(strings.TrimSpace(p.Cols))
		if err != nil || cols < 1 {
			return nil, bounds, 0, fmt.Errorf("cols must be a positive integer")
		}
	}

	if strings.TrimSpace(p.GridBounds) == "" {
		return nil, bounds, 0, fmt.Errorf("gridBounds is required")
	}
	var boundsSlice []int
	if err = json.Unmarshal([]byte(p.GridBounds), &boundsSlice); err != nil {
		return nil, bounds, 0, fmt.Errorf("gridBounds must be a JSON array of 4 integers: %w", err)
	}
	if len(boundsSlice) != 4 {
		return nil, bounds, 0, fmt.Errorf("gridBounds must have exactly 4 elements [left,top,right,bottom]")
	}
	bounds = [4]int{boundsSlice[0], boundsSlice[1], boundsSlice[2], boundsSlice[3]}
	if bounds[0] >= bounds[2] {
		return nil, bounds, 0, fmt.Errorf("gridBounds right must be greater than left")
	}
	if bounds[1] >= bounds[3] {
		return nil, bounds, 0, fmt.Errorf("gridBounds bottom must be greater than top")
	}

	return squares, bounds, cols, nil
}

func validateSquaresToTapsResult(raw json.RawMessage) error {
	var result squaresToTapsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	count, err := strconv.Atoi(result.Count)
	if err != nil || count < 0 {
		return fmt.Errorf("count must be a non-negative integer string")
	}
	return nil
}
