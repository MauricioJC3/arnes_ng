package provider

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"contexto cancelado", context.Canceled, false},
		{"deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), false},
		{"429", errors.New("openai_compat: HTTP 429"), true},
		{"503", errors.New("openai_compat: sin respuesta tras 4 intentos: HTTP 503"), true},
		{"400 no es transitorio", errors.New("openai_compat: HTTP 400: bad request"), false},
		{"401 no es transitorio", errors.New("openai_compat: HTTP 401: unauthorized"), false},
		{"stream cortado a mitad", errors.New("EOF -- el proveedor cortó el stream a mitad de la respuesta"), true},
		{"stream estancado", errors.New("openai_compat: el stream se cortó, no llegaron datos nuevos"), true},
		{"anthropic overloaded", errors.New("anthropic: overloaded_error"), true},
		{"error de negocio cualquiera", errors.New("anthropic: rol de mensaje desconocido"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Transient(tt.err); got != tt.want {
				t.Fatalf("Transient(%v) = %v, quiero %v", tt.err, got, tt.want)
			}
		})
	}
}
