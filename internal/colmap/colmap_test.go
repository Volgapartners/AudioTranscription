package colmap

import (
	"testing"
)

func TestResolve_StandardHeaders(t *testing.T) {
	headers := []string{"audio_name", "speaker", "start_time", "end_time", "transcript", "emotion", "locale"}
	cols := Resolve(headers)

	expect := map[string]int{
		ColAudioName:  0,
		ColSpeaker:    1,
		ColStartTime:  2,
		ColEndTime:    3,
		ColTranscript: 4,
		ColEmotion:    5,
		ColLocale:     6,
	}

	for key, wantIdx := range expect {
		gotIdx := cols[key]
		if gotIdx != wantIdx {
			t.Errorf("Resolve(%q): got %d, want %d", key, gotIdx, wantIdx)
		}
	}
}

func TestResolve_MessyHeaders(t *testing.T) {
	// Vendor-style headers with spaces, parens, different naming
	headers := []string{
		"File Name",            // audio_name alias
		"Speaker",              // speaker
		"Start Time (Decimal)", // start_time alias
		"End Time (Decimal)",   // end_time alias
		"Text",                 // transcript alias
		"Emotions",             // emotion alias
	}
	cols := Resolve(headers)

	if !Has(cols, ColAudioName) || cols[ColAudioName] != 0 {
		t.Errorf("expected audio_name at 0, got %d", cols[ColAudioName])
	}
	if !Has(cols, ColSpeaker) || cols[ColSpeaker] != 1 {
		t.Errorf("expected speaker at 1, got %d", cols[ColSpeaker])
	}
	if !Has(cols, ColStartTime) || cols[ColStartTime] != 2 {
		t.Errorf("expected start_time at 2, got %d", cols[ColStartTime])
	}
	if !Has(cols, ColEndTime) || cols[ColEndTime] != 3 {
		t.Errorf("expected end_time at 3, got %d", cols[ColEndTime])
	}
	if !Has(cols, ColTranscript) || cols[ColTranscript] != 4 {
		t.Errorf("expected transcript at 4, got %d", cols[ColTranscript])
	}
	if !Has(cols, ColEmotion) || cols[ColEmotion] != 5 {
		t.Errorf("expected emotion at 5, got %d", cols[ColEmotion])
	}
}

func TestResolve_MissingColumns(t *testing.T) {
	headers := []string{"speaker", "text"}
	cols := Resolve(headers)

	if Has(cols, ColAudioName) {
		t.Error("audio_name should not be found")
	}
	if !Has(cols, ColSpeaker) {
		t.Error("speaker should be found")
	}
	if !Has(cols, ColTranscript) {
		t.Error("transcript should be found via 'text' alias")
	}
	if Has(cols, ColStartTime) {
		t.Error("start_time should not be found")
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	headers := []string{"SPEAKER", "START_TIME", "END_TIME", "TRANSCRIPT"}
	cols := Resolve(headers)

	if !Has(cols, ColSpeaker) {
		t.Error("SPEAKER (uppercase) should match speaker")
	}
	if !Has(cols, ColStartTime) {
		t.Error("START_TIME (uppercase) should match start_time")
	}
	if !Has(cols, ColTranscript) {
		t.Error("TRANSCRIPT (uppercase) should match transcript")
	}
}

func TestResolve_UnderscoreVariants(t *testing.T) {
	headers := []string{"starttime_decimal", "endtime_decimal"}
	cols := Resolve(headers)

	if !Has(cols, ColStartTime) {
		t.Error("starttime_decimal should match start_time")
	}
	if !Has(cols, ColEndTime) {
		t.Error("endtime_decimal should match end_time")
	}
}

func TestHas_Negative(t *testing.T) {
	m := map[string]int{"audio_name": -1, "speaker": 0}
	if Has(m, "audio_name") {
		t.Error("Has should return false for index -1")
	}
	if !Has(m, "speaker") {
		t.Error("Has should return true for index 0")
	}
}
