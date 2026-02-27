package langutil

import (
	"testing"
)

func TestLanguageDisplay(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"en", "English"},
		{"EN", "English"},
		{"ar", "Arabic"},
		{"ja", "Japanese"},
		{"en-us", "English (US)"},
		{"en_us", "English (US)"}, // underscore should be normalised
		{"xx", "xx"},              // unknown returns as-is
		{"", ""},
	}

	for _, tt := range tests {
		got := LanguageDisplay(tt.code)
		if got != tt.want {
			t.Errorf("LanguageDisplay(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestNormalizeSpeaker(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"Speaker 1", "speaker_01"},
		{"speaker_2", "speaker_02"},
		{"SPEAKER 10", "speaker_10"},
		{"speaker", "speaker"},
		{"", ""},
		{"  Speaker 3  ", "speaker_03"},
	}

	for _, tt := range tests {
		got := NormalizeSpeaker(tt.raw)
		if got != tt.want {
			t.Errorf("NormalizeSpeaker(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestNormalizeAudioName(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"file_name.mp3", "file#name"},
		{"path/to/audio_file.wav", "audio#file"},
		{"audio_file.m4a.xlsx", "audio#file.xlsx"},
		{"no_extension", "no#extension"},
		{"already#hashed", "already#hashed"},
		{"", ""},
	}

	for _, tt := range tests {
		got := NormalizeAudioName(tt.raw)
		if got != tt.want {
			t.Errorf("NormalizeAudioName(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestApplyOverlap(t *testing.T) {
	tests := []struct {
		text string
		lang string
		want string
	}{
		{"hello [overlapping] world", "en", "hello (overlapping)overlapping(/overlapping) world"},
		{"hello <overlapping> world", "sv", "hello (overlapping)överlappande(/overlapping) world"},
		{"hello {overlapping} world", "ar", "hello (overlapping)تداخل(/overlapping) world"},
		{"no overlap", "en", "no overlap"},
		{"", "en", ""},
	}

	for _, tt := range tests {
		got := ApplyOverlap(tt.text, tt.lang)
		if got != tt.want {
			t.Errorf("ApplyOverlap(%q, %q) = %q, want %q", tt.text, tt.lang, got, tt.want)
		}
	}
}

func TestDetectLangFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/data/lang=sv/file.xlsx", "sv"},
		{"/data/lang=en-us/file.xlsx", "en-us"},
		{"/data/ja/file.xlsx", "ja"},
		{"/data/some_random/file.xlsx", ""},
	}

	for _, tt := range tests {
		got := DetectLangFromPath(tt.path)
		if got != tt.want {
			t.Errorf("DetectLangFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeLocale(t *testing.T) {
	tests := []struct {
		raw      string
		fallback string
		want     string
	}{
		{"", "en", "en-US"},
		{"", "ja", "ja-JP"},
		{"en-US", "en", "en-US"},
		{"ja_JP", "ja", "ja-JP"},
		{"ar", "ar", "ar-AR"},
		{"xx-YY", "xx", "xx-YY"}, // 5-char format accepted as-is
	}

	for _, tt := range tests {
		got := NormalizeLocale(tt.raw, tt.fallback)
		if got != tt.want {
			t.Errorf("NormalizeLocale(%q, %q) = %q, want %q", tt.raw, tt.fallback, got, tt.want)
		}
	}
}

func TestStripAudioExtensions(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file.mp3", "file"},
		{"file.wav.mp3", "file"},
		{"file.txt", "file.txt"},
		{"file", "file"},
	}

	for _, tt := range tests {
		got := StripAudioExtensions(tt.input)
		if got != tt.want {
			t.Errorf("StripAudioExtensions(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectSpeakerModeFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/data/speaker=single/lang=en/file.xlsx", "single"},
		{"/data/speaker=multi/lang=en/file.xlsx", "multi"},
		{"/data/single/file.xlsx", "single"},
		{"/data/file.xlsx", ""},
	}

	for _, tt := range tests {
		got := DetectSpeakerModeFromPath(tt.path)
		if got != tt.want {
			t.Errorf("DetectSpeakerModeFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
