package matcher

import (
	"regexp"
	"strings"
)

var (
	spaceRE         = regexp.MustCompile(`\s+`)
	trailingTokenRE = regexp.MustCompile(`(?i)\s*[\(\[\{（【].*?(live|现场|演唱会|concert|remix|mix|版|version|ver\.?|伴奏|karaoke).*?[\)\]\}）】]\s*$`)
	trailingDashRE  = regexp.MustCompile(`(?i)\s*[-–—]\s*(live|现场|演唱会|concert|remix|mix|.*?版|version|ver\.?|伴奏|karaoke)\s*$`)
	liveRE          = regexp.MustCompile(`(?i)(\blive\b|现场|演唱会|concert)`)
)

func NormalizeTitleBrief(title string) string {
	title = strings.TrimSpace(title)
	for {
		next := trailingTokenRE.ReplaceAllString(title, "")
		next = trailingDashRE.ReplaceAllString(next, "")
		next = strings.TrimSpace(next)
		if next == title {
			break
		}
		title = next
	}
	return spaceRE.ReplaceAllString(title, " ")
}

func DetectLive(title string) bool {
	return liveRE.MatchString(title)
}

func NormalizeForCompare(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = NormalizeTitleBrief(value)
	replacer := strings.NewReplacer("（", "(", "）", ")", "【", "[", "】", "]", "&", "and")
	value = replacer.Replace(value)
	value = regexp.MustCompile(`[[:punct:]\s]+`).ReplaceAllString(value, "")
	return value
}
