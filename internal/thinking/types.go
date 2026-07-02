// Package thinking provides unified thinking configuration processing.
package thinking

// ThinkingLevel represents a discrete thinking level.
type ThinkingLevel string

const (
	// LevelNone disables thinking
	LevelNone ThinkingLevel = "none"
	// LevelAuto enables automatic/dynamic thinking
	LevelAuto ThinkingLevel = "auto"
	// LevelMinimal sets minimal thinking effort
	LevelMinimal ThinkingLevel = "minimal"
	// LevelLow sets low thinking effort
	LevelLow ThinkingLevel = "low"
	// LevelMedium sets medium thinking effort
	LevelMedium ThinkingLevel = "medium"
	// LevelHigh sets high thinking effort
	LevelHigh ThinkingLevel = "high"
	// LevelXHigh sets extra-high thinking effort
	LevelXHigh ThinkingLevel = "xhigh"
	// LevelMax sets maximum thinking effort
	LevelMax ThinkingLevel = "max"
)
