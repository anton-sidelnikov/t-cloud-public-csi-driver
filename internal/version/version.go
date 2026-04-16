package version

type Info struct {
	Version string
	Commit  string
	Date    string
}

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func Get() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
	}
}
