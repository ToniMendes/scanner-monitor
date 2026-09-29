// Package domain contains the application's core domain models.
package domain

import (
	"fmt"
	"strings"
)

type Student struct {
	FullName   string
	Cpf        string
	Prontuario string
}

func (s *Student) NewStudent() (*Student, error) {
	if strings.TrimSpace(s.FullName) == "" {
		return nil, fmt.Errorf("full name not found")
	}

	if strings.TrimSpace(s.Cpf) == "" {
		return nil, fmt.Errorf("cpf not found")
	}

	if strings.TrimSpace(s.Prontuario) == "" {
		return nil, fmt.Errorf("prontuario not found")
	}

	return &Student{
		FullName:   s.FullName,
		Cpf:        s.Cpf,
		Prontuario: s.Prontuario,
	}, nil
}
