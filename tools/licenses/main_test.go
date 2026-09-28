package main

import (
	"slices"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
		err  string
	}{
		{"MIT License\n\nPermission is hereby granted, free of charge, to any person", []string{"MIT"}, ""},
		{"Redistribution and use in source and binary forms, with or without", []string{"BSD"}, ""},
		{"covered by MIT and Apache: Permission is hereby granted, free of charge ... Apache License, Version 2.0",
			[]string{"MIT", "Apache-2.0"}, ""},
		{"The author disclaims copyright. SQLite code has been dedicated to the public domain.", []string{"public domain"}, ""},
		{"# Third-Party Software Notices\n\nSee each folder.", nil, ""},
		{"GNU GENERAL PUBLIC LICENSE\nVersion 3, 29 June 2007", nil, "GNU GENERAL PUBLIC LICENSE isn't allowed"},
		{"GNU Affero General Public License", nil, "AFFERO"},
		{"Business Source License 1.1", nil, "Business Source License"},
	} {
		got, err := classify([]byte(tc.text))
		switch {
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("classify(%.30q) error = %v, want %q", tc.text, err, tc.err)
		case tc.err == "" && (err != nil || !slices.Equal(got, tc.want)):
			t.Errorf("classify(%.30q) = %v, %v; want %v", tc.text, got, err, tc.want)
		}
	}
}
