package datatruth

import "testing"

// The pep reader is a KNOWN reader (an unknown token concerns everyone, so a
// pep-scoped block that the consumer could not match would pause every reader),
// it is named for the owner, and it is held to what the mentor reader is held to
// but not to what only the AI reader reads.
func TestReaderPepIsKnownNamedAndScoped(t *testing.T) {
	if !knownReader(ReaderPep) || !knownReader(ReaderAI) || !knownReader(ReaderMentor) {
		t.Fatal("pep, ai and mentor are the known readers")
	}
	if knownReader("typo") {
		t.Fatal("fixture: a typo is not a reader")
	}
	if got := readerNames([]string{ReaderPep}); len(got) != 1 || got[0] != "pep traders" {
		t.Fatalf("readerNames(pep) = %v", got)
	}
	cases := []struct {
		readers []string
		reader  string
		want    bool
	}{
		{nil, ReaderPep, true},
		{[]string{ReaderPep}, ReaderPep, true},
		{[]string{ReaderMentor}, ReaderPep, true},
		{[]string{ReaderAI}, ReaderPep, false},
		{[]string{ReaderPep}, ReaderAI, false},
		{[]string{ReaderPep}, ReaderMentor, false},
		{[]string{ReaderMentor}, ReaderAI, false}, // unchanged
	}
	for _, c := range cases {
		if got := (Reason{Readers: c.readers}).Concerns(c.reader); got != c.want {
			t.Errorf("Reason{%v}.Concerns(%s) = %v, want %v", c.readers, c.reader, got, c.want)
		}
	}
}
