package eventprocessor

import (
	"encoding/json"
	"fmt"

	"github.com/openkcm/orbital"
)

// unmarshalKeyJobData mirrors CMK's unmarshalKeyJobData.
func unmarshalKeyJobData(job orbital.Job) (KeyActionJobData, error) {
	var data KeyActionJobData
	if err := json.Unmarshal(job.Data, &data); err != nil {
		return KeyActionJobData{}, fmt.Errorf("failed to unmarshal job data: %w", err)
	}
	return data, nil
}

// unmarshalSystemJobData mirrors CMK's unmarshalSystemJobData.
func unmarshalSystemJobData(job orbital.Job) (SystemActionJobData, error) {
	var data SystemActionJobData
	if err := json.Unmarshal(job.Data, &data); err != nil {
		return SystemActionJobData{}, fmt.Errorf("failed to unmarshal job data: %w", err)
	}
	return data, nil
}
