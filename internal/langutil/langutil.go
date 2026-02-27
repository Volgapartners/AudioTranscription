// Package langutil provides language detection, audio-name normalisation,
// speaker formatting, overlap-tag rewriting, and locale handling. These
// utilities are ported from the 683 / 751 Python converter scripts so
// the Go backend can process vendor-provided spreadsheets identically.
package langutil

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Language name mapping (superset of 683+751 scripts)
// ---------------------------------------------------------------------------

// LangNameMap maps ISO 639-1 (or locale-style) codes to display names.
var LangNameMap = map[string]string{
	"ar":    "Arabic",
	"cs":    "Czech",
	"da":    "Danish",
	"de":    "German",
	"en":    "English",
	"en-us": "English (US)",
	"es":    "Spanish",
	"fi":    "Finnish",
	"fr":    "French",
	"hi":    "Hindi",
	"hu":    "Hungarian",
	"id":    "Indonesian",
	"it":    "Italian",
	"ja":    "Japanese",
	"ko":    "Korean",
	"nl":    "Dutch",
	"no":    "Norwegian",
	"pl":    "Polish",
	"ru":    "Russian",
	"sv":    "Swedish",
	"th":    "Thai",
	"tr":    "Turkish",
	"vi":    "Vietnamese",
	"zh":    "Chinese",
}

// OverlapTokenMap maps language codes to their localised "overlapping" tag.
var OverlapTokenMap = map[string]string{
	"en":    "overlapping",
	"en-us": "overlapping",
	"ar":    "تداخل",
	"cs":    "překrývání",
	"da":    "overlappende",
	"fi":    "päällekkäinen",
	"hi":    "ओवरलैप",
	"hu":    "átfedés",
	"id":    "tumpang tindih",
	"it":    "sovrapposizione",
	"ja":    "重なり",
	"ko":    "겹침",
	"nl":    "overlappend",
	"no":    "overlappende",
	"pl":    "nakładanie",
	"ru":    "перекрытие",
	"sv":    "överlappande",
	"th":    "ทับซ้อน",
	"tr":    "örtüşme",
	"vi":    "chồng lấn",
	"zh":    "重叠",
}

// allowedLangs is the set of recognised language codes.
var allowedLangs map[string]bool

func init() {
	allowedLangs = make(map[string]bool, len(LangNameMap))
	for k := range LangNameMap {
		allowedLangs[k] = true
	}
}

// ---------------------------------------------------------------------------
// Audio extension handling
// ---------------------------------------------------------------------------

var audioExtRE = regexp.MustCompile(`(?i)\.(mp3|wav|m4a|flac|ogg|aac|wma|mp4|webm|opus)`)

// StripAudioExtensions removes audio file extensions anywhere in the string.
func StripAudioExtensions(s string) string {
	return audioExtRE.ReplaceAllString(s, "")
}

// ---------------------------------------------------------------------------
// Language detection from path
// ---------------------------------------------------------------------------

var langFolderRE = regexp.MustCompile(`(?i)\blang=([a-z]{2,3}(?:-[a-z]{2})?)\b`)

// canonLang normalises a language string: lowercase, underscores → dashes.
func canonLang(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
}

// extractLangFromFolder tries to find a language code in a single folder name.
func extractLangFromFolder(folder string) string {
	f := canonLang(folder)

	// Explicit "lang=xx"
	if m := langFolderRE.FindStringSubmatch(f); len(m) > 1 {
		return canonLang(m[1])
	}

	// Bare code (folder is exactly "ja", "en-us", etc.)
	if allowedLangs[f] {
		return f
	}

	// Substring match — longest code first to prefer "en-us" over "en"
	for _, code := range sortedLangs() {
		escaped := regexp.QuoteMeta(code)
		re := regexp.MustCompile(`(?:^|[^a-z0-9])` + escaped + `(?:[^a-z0-9]|$)`)
		if re.MatchString(f) {
			return code
		}
	}
	return ""
}

// sortedLangs returns language codes sorted by length descending so that
// longer codes like "en-us" are matched before shorter ones like "en".
func sortedLangs() []string {
	codes := make([]string, 0, len(allowedLangs))
	for k := range allowedLangs {
		codes = append(codes, k)
	}
	// Simple insertion sort — tiny list, called rarely.
	for i := 1; i < len(codes); i++ {
		for j := i; j > 0 && len(codes[j]) > len(codes[j-1]); j-- {
			codes[j], codes[j-1] = codes[j-1], codes[j]
		}
	}
	return codes
}

// DetectLangFromPath walks the path components from right to left and returns
// the first recognised language code, or "" if none is found.
func DetectLangFromPath(path string) string {
	abs, _ := filepath.Abs(path)
	parts := strings.Split(filepath.ToSlash(abs), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if code := extractLangFromFolder(parts[i]); code != "" {
			return code
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Speaker mode detection
// ---------------------------------------------------------------------------

var speakerModeRE = regexp.MustCompile(`(?i)\bspeaker=(single|multi)\b`)

// DetectSpeakerModeFromPath extracts "single" or "multi" from the path.
func DetectSpeakerModeFromPath(path string) string {
	abs, _ := filepath.Abs(path)
	parts := strings.Split(filepath.ToSlash(abs), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		f := strings.ToLower(parts[i])
		if m := speakerModeRE.FindStringSubmatch(f); len(m) > 1 {
			return m[1]
		}
		if f == "single" || f == "multi" {
			return f
		}
		if matched, _ := regexp.MatchString(`\bsingle\b`, f); matched {
			return "single"
		}
		if matched, _ := regexp.MatchString(`\bmulti\b`, f); matched {
			return "multi"
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Display name helpers
// ---------------------------------------------------------------------------

// LanguageDisplay returns the human-readable language name for a code,
// or the code itself if not found.
func LanguageDisplay(code string) string {
	c := canonLang(code)
	if name, ok := LangNameMap[c]; ok {
		return name
	}
	return c
}

// ---------------------------------------------------------------------------
// Audio name normalisation
// ---------------------------------------------------------------------------

// NormalizeAudioName takes a raw value (filename, path, or cell value) and
// produces the canonical audio name: basename only, no extension, underscores
// replaced with "#".
func NormalizeAudioName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = StripAudioExtensions(s)
	s = strings.ReplaceAll(s, "_", "#")
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// Speaker normalisation
// ---------------------------------------------------------------------------

var speakerDigitRE = regexp.MustCompile(`\d+`)

// NormalizeSpeaker extracts the first number from a speaker value and formats
// it as "speaker_XX". If no number is found, the value is lowercased.
func NormalizeSpeaker(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if m := speakerDigitRE.FindString(s); m != "" {
		// Parse and re-format with zero-padding.
		n := 0
		for _, c := range m {
			n = n*10 + int(c-'0')
		}
		return fmt.Sprintf("speaker_%02d", n)
	}
	return strings.ToLower(s)
}

// ---------------------------------------------------------------------------
// Overlap tag rewriting
// ---------------------------------------------------------------------------

var overlapRE = regexp.MustCompile(`(?i)([\[<{])\s*overlapping\s*([\]>}])`)

// ApplyOverlap rewrites overlap markers like [overlapping] or <overlapping>
// into the client format: (overlapping)TOKEN(/overlapping).
func ApplyOverlap(text, lang string) string {
	if text == "" {
		return text
	}
	token, ok := OverlapTokenMap[canonLang(lang)]
	if !ok {
		token = "overlapping"
	}
	replacement := "(overlapping)" + token + "(/overlapping)"
	return overlapRE.ReplaceAllString(text, replacement)
}

// ---------------------------------------------------------------------------
// Locale normalisation
// ---------------------------------------------------------------------------

// ForcedLocaleByLang maps a base language code to the canonical locale.
var ForcedLocaleByLang = map[string]string{
	"en":    "en-US",
	"en-us": "en-US",
	"ar":    "ar-AR",
	"cs":    "cs-CZ",
	"da":    "da-DK",
	"de":    "de-DE",
	"es":    "es-ES",
	"fi":    "fi-FI",
	"fr":    "fr-FR",
	"hi":    "hi-IN",
	"hu":    "hu-HU",
	"id":    "id-ID",
	"it":    "it-IT",
	"ja":    "ja-JP",
	"ko":    "ko-KR",
	"nl":    "nl-NL",
	"no":    "nb-NO",
	"pl":    "pl-PL",
	"ru":    "ru-RU",
	"sv":    "sv-SE",
	"th":    "th-TH",
	"tr":    "tr-TR",
	"vi":    "vi-VN",
	"zh":    "zh-CN",
}

// NormalizeLocale validates and corrects a raw locale value. If the raw
// value is empty or unrecognised, it falls back per language code.
func NormalizeLocale(rawLocale, fallbackLang string) string {
	raw := strings.TrimSpace(rawLocale)
	if raw == "" {
		if loc, ok := ForcedLocaleByLang[canonLang(fallbackLang)]; ok {
			return loc
		}
		return fallbackLang
	}
	raw = strings.ReplaceAll(raw, "_", "-")
	// If it looks valid (xx-YY), accept it.
	if len(raw) == 5 && raw[2] == '-' {
		return raw
	}
	// Try to fix from the base language.
	lang := strings.Split(raw, "-")[0]
	if loc, ok := ForcedLocaleByLang[strings.ToLower(lang)]; ok {
		return loc
	}
	return raw
}

// LangFolderName returns a folder name like "lang=sv" for a given path.
func LangFolderName(path string) string {
	code := DetectLangFromPath(path)
	if code != "" {
		return "lang=" + code
	}
	return "_unknown_lang"
}

// SpeakerFolderName returns a folder name like "speaker=single" for a path.
func SpeakerFolderName(path string) string {
	mode := DetectSpeakerModeFromPath(path)
	if mode != "" {
		return "speaker=" + mode
	}
	return "_unknown_speaker"
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// FileExists is a small convenience to check if a path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
