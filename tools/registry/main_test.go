package main

import "testing"

func TestParseIssue(t *testing.T) {
	body := "### Recipe name\n\n@dasdarki/rp\n\n### Repository\n\nhttps://github.com/DasDarki/rp\n\n### Subdirectory\n\n_No response_\n\n### Keywords\n\nrp, db\n"
	fields := parseIssue(body)
	if fields["Recipe name"] != "@dasdarki/rp" || fields["Repository"] != "https://github.com/DasDarki/rp" {
		t.Errorf("unexpected fields: %v", fields)
	}
	if fields["Subdirectory"] != "" || fields["Keywords"] != "rp, db" {
		t.Errorf("unexpected optional fields: %v", fields)
	}
}

func TestParseRecipefile(t *testing.T) {
	manifest := parseRecipefile("# comment\nrecipe @dasdarki/rp 1.0.0\ndescription \"Roleplay server\"\nkeywords rp db\npackage sandbox\n")
	if manifest.Recipe.Name != "@dasdarki/rp" || manifest.Recipe.Description != "Roleplay server" || len(manifest.Recipe.Keywords) != 2 {
		t.Errorf("unexpected manifest: %+v", manifest.Recipe)
	}
}
