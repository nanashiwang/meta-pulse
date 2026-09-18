package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nanashiwang/meta-pulse/internal/service"
)

func readPeriodRewardsFile(path string) ([]service.PeriodRewardSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open reward file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<10+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<10 {
		return nil, errors.New("reward file exceeds 64 KiB")
	}
	return parsePeriodRewards(data)
}

func parsePeriodRewards(data []byte) ([]service.PeriodRewardSpec, error) {
	fail := errors.New("reward file must be a JSON array of 1-50 objects containing exactly key, amount and weight once each")
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, fail
	}
	var result []service.PeriodRewardSpec
	for d.More() {
		if len(result) >= 50 {
			return nil, fail
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('{') {
			return nil, fail
		}
		var item service.PeriodRewardSpec
		seen := map[string]bool{}
		for d.More() {
			token, err = d.Token()
			if err != nil {
				return nil, fail
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return nil, fail
			}
			seen[key] = true
			switch key {
			case "key":
				err = d.Decode(&item.Key)
			case "amount":
				err = d.Decode(&item.Amount)
			case "weight":
				err = d.Decode(&item.Weight)
			default:
				return nil, fail
			}
			if err != nil {
				return nil, fail
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('}') || len(seen) != 3 {
			return nil, fail
		}
		result = append(result, item)
	}
	token, err = d.Token()
	if err != nil || token != json.Delim(']') || len(result) == 0 {
		return nil, fail
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return nil, fail
	}
	return result, nil
}
