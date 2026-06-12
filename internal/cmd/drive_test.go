package cmd

import "testing"

func TestMimeForAs(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "text/markdown", false},
		{"md", "text/markdown", false},
		{"markdown", "text/markdown", false},
		{"MD", "text/markdown", false},
		{"txt", "text/plain", false},
		{"text", "text/plain", false},
		{"plain", "text/plain", false},
		{"html", "text/html", false},
		{" HTML ", "text/html", false},
		{"pdf", "application/pdf", false},
		{"doc", "", true},
		{"json", "", true},
	}
	for _, c := range cases {
		got, err := mimeForAs(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("mimeForAs(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("mimeForAs(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("mimeForAs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDriveFileID(t *testing.T) {
	const id = "11_hJr42_5gxtuyIsGB3_AjsTwSymFwdUa8G_M-hsUrg"
	cases := []struct {
		in   string
		want string
	}{
		{id, id},
		{"  " + id + "  ", id},
		{"https://docs.google.com/document/d/" + id + "/edit?usp=drivesdk", id},
		{"https://docs.google.com/spreadsheets/d/" + id + "/edit#gid=0", id},
		{"https://docs.google.com/presentation/d/" + id + "/edit", id},
		{"https://drive.google.com/file/d/" + id + "/view?usp=sharing", id},
		{"https://drive.google.com/open?id=" + id, id},
		{"https://drive.google.com/uc?id=" + id + "&export=download", id},
		{"https://docs.google.com/document/d/" + id, id},
	}
	for _, c := range cases {
		if got := driveFileID(c.in); got != c.want {
			t.Errorf("driveFileID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
