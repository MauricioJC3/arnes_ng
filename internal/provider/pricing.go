package provider

import "strings"

// priceTable is USD per million tokens (input, output). Approximate -- update as
// provider rates change. Unknown models fall back to no cost display.
var priceTable = map[string][2]float64{
	// Anthropic
	"claude-opus-5":     {5, 25},
	"claude-opus-4-8":   {5, 25},
	"claude-opus-4-7":   {5, 25},
	"claude-opus-4-6":   {5, 25},
	"claude-sonnet-5":   {2, 10},
	"claude-sonnet-4-6": {3, 15},
	"claude-haiku-4-5":  {1, 5},
	"claude-fable-5":    {10, 50},

	// DeepSeek V4 (peak, cache-miss input rates)
	"deepseek-v4-flash":            {0.44, 1.32},
	"deepseek-v4-pro":              {1.32, 3.96},
	"deepseek-v4-flash-vision-exp": {0.44, 1.32},

	// Kimi / Moonshot
	"kimi-k2":          {0.60, 2.50},
	"moonshot-v1-8k":   {0.20, 2.00},
	"moonshot-v1-32k":  {1.00, 3.00},
	"moonshot-v1-128k": {2.00, 5.00},

	// OpenAI
	"gpt-4o":       {2.50, 10},
	"gpt-4o-mini":  {0.15, 0.60},
	"gpt-4.1":      {2.00, 8.00},
	"gpt-4.1-mini": {0.40, 1.60},
}

// Price returns the USD-per-million-tokens rates for a model. ok is false when
// the model has no known price.
func Price(model string) (inUSD, outUSD float64, ok bool) {
	if p, found := priceTable[strings.ToLower(strings.TrimSpace(model))]; found {
		return p[0], p[1], true
	}
	return 0, 0, false
}

// Cost returns the USD cost of a request given token counts, and ok=false when
// the model's price is unknown.
func Cost(model string, inTokens, outTokens int) (usd float64, ok bool) {
	pin, pout, ok := Price(model)
	if !ok {
		return 0, false
	}
	return float64(inTokens)/1e6*pin + float64(outTokens)/1e6*pout, true
}
