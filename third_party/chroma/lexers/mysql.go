package lexers

import (
	"regexp"
)

func analyseMySQL() {
	mysqlAnalyserNameBetweenBacktickRe := regexp.MustCompile("`[a-zA-Z_]\\w*`")
	mysqlAnalyserNameBetweenBracketRe := regexp.MustCompile(`\[[a-zA-Z_]\w*\]`)
	GlobalLexerRegistry.Get("mysql").
		SetAnalyser(func(text string) float32 {
			nameBetweenBacktickCount := len(mysqlAnalyserNameBetweenBacktickRe.FindAllString(text, -1))
			nameBetweenBracketCount := len(mysqlAnalyserNameBetweenBracketRe.FindAllString(text, -1))

			var result float32

			dialectNameCount := nameBetweenBacktickCount + nameBetweenBracketCount
			if dialectNameCount >= 1 && nameBetweenBacktickCount >= (2*nameBetweenBracketCount) {

				result += 0.5
			} else if nameBetweenBacktickCount > nameBetweenBracketCount {
				result += 0.2
			} else if nameBetweenBacktickCount > 0 {
				result += 0.1
			}

			return result
		})
}
