package main

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const instructionsVersion = "runtime-v1"

func assistantRunID(role, runID string) string {
	if role == "assistant" {
		return runID
	}
	return ""
}
func runtimeInstructions() string {
	return instructionsAt(time.Now())
}
func instructionsAt(now time.Time) string {
	return fmt.Sprintf("You are Agent. Current date: %s. Server timezone: %s; UTC offset: %s. Use this trusted runtime date for relative dates. A gap in your knowledge does not mean an event is in the future. Do not claim live verification: no browsing or other tools are available in this chat. State uncertainty when current facts cannot be verified.", now.Format("2006-01-02"), now.Location().String(), now.Format("-07:00"))
}
