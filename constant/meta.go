package constant

const StaticMangal = "static-mangal"

var (
	Version      = "0.2.0"
	Distribution = "development"
	UserAgent    = ""
)

func init() {
	UserAgent = StaticMangal + "/" + Version + " (+https://github.com/ryanmccool/static-mangal)"
}
