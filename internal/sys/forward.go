package sys

import (
	"context"
	"database/sql"
	iofs "io/fs"
	"os"
	"regexp"
	"testing"
	"time"

	"tofu/internal/platform/build"
	"tofu/internal/platform/clipboard"
	"tofu/internal/platform/console"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
	"tofu/internal/platform/profile"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/platform/scratch"
)

const (
	StateDirName       = fs.StateDirName
	LegacyStateDirName = fs.LegacyStateDirName
	ProjectsDirName    = fs.ProjectsDirName
	QuotaDirName       = fs.QuotaDirName
	DeskRoot           = fs.DeskRoot
	ProductSymbol      = fs.ProductSymbol
)

type Layer = fs.Layer

func OS() string                                      { return fs.OS() }
func Arch() string                                    { return fs.Arch() }
func Join(elem ...string) string                      { return fs.Join(elem...) }
func StateDir(parent string) string                   { return fs.StateDir(parent) }
func HomeConfigDir() (string, error)                  { return fs.HomeConfigDir() }
func SourceRoot() string                              { return fs.SourceRoot() }
func IsOwnerCredential(path string) bool              { return fs.IsOwnerCredential(path) }
func MovedStateNames() []string                       { return fs.MovedStateNames() }
func QuotaDir() (string, error)                       { return fs.QuotaDir() }
func LibraryDir() (string, error)                     { return fs.LibraryDir() }
func ReadFile(path string) ([]byte, error)            { return fs.ReadFile(path) }
func Exists(path string) (bool, error)                { return fs.Exists(path) }
func IsDir(path string) (bool, error)                 { return fs.IsDir(path) }
func ListFiles(dir, suffix string) ([]string, error)  { return fs.ListFiles(dir, suffix) }
func WalkDir(dir string, fn iofs.WalkDirFunc) error   { return fs.WalkDir(dir, fn) }
func DirLayer(name, dir string) Layer                 { return fs.DirLayer(name, dir) }
func LazyRegexp(pattern string) func() *regexp.Regexp { return fs.LazyRegexp(pattern) }
func TryLock(path string) (*os.File, bool, error)     { return fs.TryLock(path) }
func Unlock(file *os.File) error                      { return fs.Unlock(file) }
func GoRoots() []string                               { return fs.GoRoots() }
func ProductPackage(root string) (string, error)      { return fs.ProductPackage(root) }

func WriteFile(path string, data []byte, perm os.FileMode) error {
	return fs.WriteFile(path, data, perm)
}

func Layers(shipped iofs.FS, sub, dir string) ([]Layer, error) {
	return fs.Layers(shipped, sub, dir)
}

type (
	Places  = project.Places
	Project = project.Project
)

func CalibrationDir() (string, error)              { return project.CalibrationDir() }
func CrashDir(dir string) string                   { return project.CrashDir(dir) }
func Crashed(dir string, err error) error          { return project.Crashed(dir, err) }
func Crashes(dir string) safe.Report               { return project.Crashes(dir) }
func FoldsCase() bool                              { return project.FoldsCase() }
func HoldProjectOpen(dir string) (release func())  { return project.HoldProjectOpen(dir) }
func LogDir() (string, error)                      { return project.LogDir() }
func OpenProject(dir string) (Project, error)      { return project.OpenProject(dir) }
func OwnerProjectStateDir(dir string) string       { return project.OwnerProjectStateDir(dir) }
func ProjectStateDir() (string, error)             { return project.ProjectStateDir() }
func ProjectStateDirAt(dir string) (string, error) { return project.ProjectStateDirAt(dir) }
func RecordedStateDir(elem ...string) string       { return project.RecordedStateDir(elem...) }
func RecordIdentity(dir string) error              { return project.RecordIdentity(dir) }
func ResolvePlaces(dir string) (Places, error)     { return project.ResolvePlaces(dir) }

func ProjectGlob(projects string, pattern ...string) ([]string, error) {
	return project.ProjectGlob(projects, pattern...)
}

func ReapTemporaryProjects(home string, now time.Time) ([]string, error) {
	return project.ReapTemporaryProjects(home, now)
}

type Clipboard = clipboard.Clipboard

const (
	ClipboardEmpty    = clipboard.ClipboardEmpty
	ClipboardText     = clipboard.ClipboardText
	ClipboardImage    = clipboard.ClipboardImage
	ClipboardFiles    = clipboard.ClipboardFiles
	ClipboardMaxBytes = clipboard.ClipboardMaxBytes
)

var ErrNoLocalClipboard = clipboard.ErrNoLocalClipboard

func ReadClipboard() (Clipboard, error)      { return clipboard.ReadClipboard() }
func WriteClipboardText(text string) error   { return clipboard.WriteClipboardText(text) }
func EnableVT(fd uintptr) error              { return console.EnableVT(fd) }
func ReadConsolePages() console.ConsolePages { return console.ReadConsolePages() }

type (
	ScratchFolder = scratch.ScratchFolder
	ScratchPlace  = scratch.ScratchPlace
	ScratchReport = scratch.ScratchReport
	ScratchSweep  = scratch.ScratchSweep
)

const (
	ScratchAgents       = scratch.ScratchAgents
	ScratchCache        = scratch.ScratchCache
	ScratchShared       = scratch.ScratchShared
	ScratchRootsSetting = scratch.ScratchRootsSetting
	ScratchKindLeftover = scratch.ScratchKindLeftover
	SessionLockFile     = scratch.SessionLockFile
)

func FirstAbsoluteRoot(roots string) string               { return scratch.FirstAbsoluteRoot(roots) }
func OncePerDay(stamp string, now time.Time, work func()) { scratch.OncePerDay(stamp, now, work) }
func ReadScratch(dir string) (ScratchReport, error)       { return scratch.ReadScratch(dir) }
func ScratchEnv(ctx context.Context) []string             { return scratch.ScratchEnv(ctx) }
func ScratchOf(ctx context.Context) (ScratchPlace, bool)  { return scratch.ScratchOf(ctx) }
func ScratchRootAt(dir string) (string, error)            { return scratch.ScratchRootAt(dir) }

func RemoveAged(dir, prefix, suffix string, before time.Time) error {
	return scratch.RemoveAged(dir, prefix, suffix, before)
}

func RemoveScratch(folders []ScratchFolder, stop func(dir string), tries int, wait time.Duration) error {
	return scratch.RemoveScratch(folders, stop, tries, wait)
}

func RemoveUnreferencedArtifacts(artifacts, sessions string, before time.Time) error {
	return scratch.RemoveUnreferencedArtifacts(artifacts, sessions, before)
}

func ScratchFor(dir, sessionID, name, tag string) (ScratchPlace, error) {
	return scratch.ScratchFor(dir, sessionID, name, tag)
}

func WithScratch(ctx context.Context, place ScratchPlace) context.Context {
	return scratch.WithScratch(ctx, place)
}

type KeyRedactor = credential.KeyRedactor

const (
	BraveSearchKeyName          = credential.BraveSearchKeyName
	CredentialFileName          = credential.CredentialFileName
	CredentialStoreName         = credential.CredentialStoreName
	KeyRedactedMark             = credential.KeyRedactedMark
	MetaMuseKeyName             = credential.MetaMuseKeyName
	OpenRouterClassifierKeyName = credential.OpenRouterClassifierKeyName
	OpenRouterKeyName           = credential.OpenRouterKeyName
	TypeSafeKeyName             = credential.TypeSafeKeyName
)

func AllowLiveCredential(tb testing.TB)                    { credential.AllowLiveCredential(tb) }
func CredentialsHiddenFromTests() bool                     { return credential.CredentialsHiddenFromTests() }
func CredentialStorePath() (string, error)                 { return credential.CredentialStorePath() }
func KeyNames() []string                                   { return credential.KeyNames() }
func KeyRedactorOf(stored map[string]string) KeyRedactor   { return credential.KeyRedactorOf(stored) }
func LoadKeyRedactor() KeyRedactor                         { return credential.LoadKeyRedactor() }
func MigrateHomeKeys() (credential.KeyMigration, error)    { return credential.MigrateHomeKeys() }
func OpenCredentialStore(path string) (*sql.DB, error)     { return credential.OpenCredentialStore(path) }
func ReadCredential(path string) ([]byte, error)           { return credential.ReadCredential(path) }
func ReadStoredKeys(db *sql.DB) (map[string]string, error) { return credential.ReadStoredKeys(db) }
func RemoveKey(name string) (bool, error)                  { return credential.RemoveKey(name) }
func SaveKey(name, value string) error                     { return credential.SaveKey(name, value) }
func StoredKeys() (map[string]string, error)               { return credential.StoredKeys() }

func CredentialAssignment(body, name string) string {
	return credential.CredentialAssignment(body, name)
}

func EqualsOwnerCredential(name, value string) bool {
	return credential.EqualsOwnerCredential(name, value)
}

func PlantOwnerCredential(tb testing.TB, name string) bool {
	return credential.PlantOwnerCredential(tb, name)
}

const (
	SnapshotEvery = profile.SnapshotEvery
	SnapshotsKept = profile.SnapshotsKept
	DevelVersion  = build.DevelVersion
)

func StartProfile() (stop func()) { return profile.StartProfile() }

func StartSnapshots(state string) (dir string, stop func(), err error) {
	return profile.StartSnapshots(state)
}

func BuildRevision() string { return build.BuildRevision() }
func GoVersion() string     { return build.GoVersion() }
func Version() string       { return build.Version() }

func NewestDesk(root, named string) (string, time.Time, error) {
	return build.NewestDesk(root, named)
}
