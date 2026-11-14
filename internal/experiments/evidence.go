package experiments

import (
	"encoding/csv"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/research"
)

// CompareCSV requires a frozen numeric criterion; completion alone cannot establish success.
func CompareCSV(data []byte, p domain.Plan, conditions domain.Conditions) (*float64, string, string, error) {
	diff := research.Compare(p.Conditions, conditions)
	if len(diff) > 0 {
		return nil, "incomparable", "evaluation_conditions_differ_or_missing", nil
	}
	reader := csv.NewReader(strings.NewReader(string(data)))
	header, err := reader.Read()
	if err != nil {
		return nil, "pending", "", errors.New("invalid_csv")
	}
	metric := -1
	for i, h := range header {
		if strings.TrimSpace(h) == p.Metric {
			metric = i
		}
	}
	if metric < 0 {
		return nil, "pending", "metric_missing", nil
	}
	values := []float64{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || metric >= len(row) {
			return nil, "pending", "", errors.New("invalid_csv_row")
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(row[metric]), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, "pending", "", errors.New("invalid_metric")
		}
		values = append(values, value)
	}
	if len(values) != 1 {
		return nil, "pending", "select_single_metric_row", nil
	}
	actual := values[0]
	if p.Expected == nil || !p.Confirmed || p.ConfirmedHash != Hash(p) {
		return &actual, "pending", "criterion_not_frozen", nil
	}
	if math.Abs(actual-*p.Expected) <= p.Tolerance {
		return &actual, "matched", "selected_target_within_confirmed_tolerance", nil
	}
	return &actual, "different", "outside_confirmed_tolerance", nil
}
