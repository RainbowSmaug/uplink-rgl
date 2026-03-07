package library

type Game struct {
    ID       string
    Name     string
    ExePath  string
    CoverURL string
    Source   string // "steam", "manual", etc.
}
