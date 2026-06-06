// Package transaction is a faithful Go port of iSarabjitDhiman/XClientTransaction.
//
// It generates the `x-client-transaction-id` header that x.com expects on its
// internal GraphQL / REST endpoints. Sending a correct transaction id on every
// authenticated request makes traffic look like a real browser session, which
// is the single biggest factor in keeping `auth_token` cookies alive instead of
// getting them flagged and expired early.
package transaction

import "regexp"

const (
	// additionalRandomNumber is the static byte appended to the payload before
	// the XOR masking step. Ported from constants.ADDITIONAL_RANDOM_NUMBER.
	additionalRandomNumber = 3

	// defaultKeyword is the obfuscation keyword folded into the SHA-256 input.
	// Ported from constants.DEFAULT_KEYWORD.
	defaultKeyword = "obfiowerehiring"

	// onDemandFileURLTemplate builds the ondemand.s JS file URL from its hash.
	onDemandFileURLTemplate = "https://abs.twimg.com/responsive-web/client-web/ondemand.s.%sa.js"

	// epochOffsetMillis is subtracted from the current unix-millis timestamp.
	// 1682924400 seconds ~= 2023-04-23T00:00:00Z. Ported from the literal
	// 1682924400 * 1000 in generate_transaction_id.
	epochOffsetMillis = 1682924400 * 1000

	// totalTime is the animation frame-time normaliser.
	totalTime = 4096
)

var (
	// onDemandFileRegex finds the ondemand.s file index in the home page HTML.
	// Python: r""",(\d+):["']ondemand\.s["']"""
	onDemandFileRegex = regexp.MustCompile(`,(\d+):["']ondemand\.s["']`)

	// indicesRegex extracts the key-byte indices from the ondemand.s JS file.
	// Python: r"""(\(\w{1}\[(\d{1,2})\],\s*16\))+"""
	indicesRegex = regexp.MustCompile(`\(\w\[(\d{1,2})\],\s*16\)`)
)

// onDemandHashPattern builds the per-index hash extraction regex. Python:
// r',{}:\"([0-9a-f]+)\"'.
func onDemandHashPattern(index string) *regexp.Regexp {
	return regexp.MustCompile(`,` + regexp.QuoteMeta(index) + `:"([0-9a-f]+)"`)
}
