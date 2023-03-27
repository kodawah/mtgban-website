package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
)

const (
	CCEstimateURL = "https://cardconduit.com/api/v1.0/estimate"
)

type CCItem struct {
	ScryfallID string `json:"scryfall_id"`
	Condition  string `json:"condition,omitempty"`
	Quantity   int    `json:"quantity,omitempty"`
	Language   string `json:"language,omitempty"`
	IsFoil     bool   `json:"is_foil,omitempty"`
	IsEtched   bool   `json:"is_etched,omitempty"`
}

type CCPayload struct {
	Items []CCItem `json:"items"`
}

type CCResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	HTTPCode int    `json:"http_code"`
	Data     struct {
		Estimate struct {
			ID        string    `json:"id"`
			URL       string    `json:"url"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"estimate"`
	} `json:"data"`
}

func sendCardConduitEstimate(items []CCItem) (string, error) {
	var payload CCPayload
	payload.Items = items
	reqBytes, err := json.Marshal(&payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, CCEstimateURL, bytes.NewReader(reqBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+Config.Api["cardconduit"])
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := cleanhttp.DefaultClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var response CCResponse
	err = json.Unmarshal(data, &response)
	if err != nil {
		return "", err
	}

	if !response.Success {
		return "", errors.New(response.Message)
	}

	return response.Data.Estimate.URL, nil
}
