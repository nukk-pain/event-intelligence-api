package fetch

// ProductionAllowedHosts is the complete host allowlist the production ingest
// command crawls. It lives here (not inline in cmd/eventsintel) so that
// eventscout's promotion output can diff discovered candidate hosts against
// the exact list ingest enforces. Growing this list is a reviewed code change
// on purpose: a host only becomes crawlable when a human commits it.
var ProductionAllowedHosts = []string{
	"www.coex.co.kr",
	"www.kintex.com",
	"showala.com",
	"icml.cc",
	"2026.aclweb.org",
	"kdd2026.kdd.org",
	"2026.ijcai.org",
	"s2026.siggraph.org",
	"www.aies-conference.com",
	"colmweb.org",
	"2026.emnlp.org",
	"neurips.cc",
	"aaai.org",
	"iclr.cc",
	"www.thecvf.com",
	"www.ces.tech",
	"www.nvidia.com",
	"convention.bio.org",
	"websummit.com",
	"qatar.websummit.com",
	"slush.org",
	"techcrunch.com",
	"worldsummit.ai",
	"hlth.com",
	"themedtechconference.com",
	"www.medica-tradefair.com",
	"jcd-expo.jp",
	"www.medicaltaiwan.com.tw",
	"www.himssconference.com",
	"automatica-munich.com",
	"www.roboticssummit.com",
	"www.switchsg.org",
	"www.rsna.org",
	"www.computextaipei.com.tw",
	"vivatechnology.com",
	"www.worldhealthexpo.com",
	"informaconnect.com",
	"www.ai-expo.net",
	"2026.ieee-icra.org",
	"www.himss.org",
	"www.humanx.co",
	"www.ieee-ras.org",
	"www.gitexeurope.com",
	"www.worldaic.com.cn",
	"www.worldrobotconference.com",
	// Promoted via eventscout -promote on 2026-07-28 (AIIA notice board; the
	// www subdomain does not respond, the bare domain is canonical).
	"k-ai.or.kr",
}
