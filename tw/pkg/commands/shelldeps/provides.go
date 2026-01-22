package shelldeps

// PackageProvides maps package names to the commands they provide.
// This is used to determine if a package's runtime dependencies
// satisfy the commands needed by its shell scripts.
//
// Note: This is not exhaustive - it covers common packages used in
// Wolfi/Chainguard images. Commands provided by busybox are limited
// versions that may not support all flags (see gnucompat.go).
var PackageProvides = map[string][]string{
	// Core utilities
	"busybox": {
		// Shells
		"sh", "ash",
		// File operations
		"cat", "cp", "mv", "rm", "ln", "ls", "mkdir", "rmdir", "touch",
		"chmod", "chown", "chgrp", "stat", "readlink", "realpath",
		"find", "xargs", "file",
		// Text processing
		"grep", "egrep", "fgrep", "sed", "awk", "cut", "sort", "uniq",
		"head", "tail", "wc", "tr", "tee", "comm", "diff", "patch",
		// Archive/compression
		"tar", "gzip", "gunzip", "zcat", "bzip2", "bunzip2", "bzcat",
		"xz", "unxz", "xzcat",
		// Network
		"wget", "nc", "netstat", "hostname", "ip", "ifconfig", "route",
		"ping", "traceroute", "nslookup",
		// Process management
		"ps", "kill", "killall", "pgrep", "pkill", "nice", "nohup",
		"timeout", "watch",
		// System info
		"uname", "uptime", "free", "df", "du", "mount", "umount",
		"id", "whoami", "groups", "date", "cal",
		// Shell utilities
		"env", "printenv", "which", "dirname", "basename", "expr",
		"seq", "sleep", "true", "false", "yes", "nproc",
		// Editors/pagers
		"vi", "less", "more",
		// Misc
		"md5sum", "sha256sum", "sha512sum", "base64", "od", "hexdump",
		"strings", "mktemp", "sync", "logger",
	},

	// GNU coreutils - provides full-featured versions
	"coreutils": {
		"cat", "cp", "mv", "rm", "ln", "ls", "mkdir", "rmdir", "touch",
		"chmod", "chown", "chgrp", "stat", "readlink", "realpath",
		"cut", "sort", "uniq", "head", "tail", "wc", "tr", "tee",
		"comm", "diff", "df", "du", "date", "env", "printenv",
		"dirname", "basename", "expr", "seq", "sleep", "true", "false",
		"yes", "nproc", "md5sum", "sha256sum", "sha512sum", "base64",
		"od", "mktemp", "sync", "id", "whoami", "groups", "uname",
		"nice", "nohup", "timeout", "install", "shred", "truncate",
		"numfmt", "factor", "expand", "unexpand", "fold", "fmt",
		"join", "paste", "split", "csplit", "nl", "pr", "ptx",
		"stty", "tty",
	},

	// Shells
	"bash": {"bash"},
	"dash": {"dash"},
	// Note: busybox also provides "sh" and "ash" (defined above in busybox entry)

	// Text processing
	"grep":     {"grep", "egrep", "fgrep"},
	"gawk":     {"awk", "gawk"},
	"mawk":     {"awk", "mawk"},
	"sed":      {"sed"},
	"diffutils": {"diff", "diff3", "sdiff", "cmp"},

	// Networking tools
	"curl":         {"curl"},
	"wget":         {"wget"},
	"bind-tools":   {"dig", "nslookup", "host", "nsupdate"},
	"iputils":      {"ping", "ping6", "tracepath", "clockdiff", "arping"},
	"iproute2":     {"ip", "ss", "tc", "bridge", "devlink", "rtmon"},
	"net-tools":    {"netstat", "ifconfig", "route", "arp", "hostname"},
	"netcat-openbsd": {"nc", "netcat"},
	"socat":        {"socat"},
	"openssh":      {"ssh", "scp", "sftp", "ssh-keygen", "ssh-keyscan"},
	"openssh-client": {"ssh", "scp", "sftp", "ssh-keygen", "ssh-keyscan"},
	"rsync":        {"rsync"},

	// Compression
	"gzip":  {"gzip", "gunzip", "zcat"},
	"bzip2": {"bzip2", "bunzip2", "bzcat"},
	"xz":    {"xz", "unxz", "xzcat", "lzma", "unlzma"},
	"zstd":  {"zstd", "unzstd", "zstdcat", "zstdmt"},
	"zip":   {"zip"},
	"unzip": {"unzip"},

	// Archive
	"tar":   {"tar"},
	"cpio":  {"cpio"},

	// Process/system utilities
	"procps":      {"ps", "top", "free", "vmstat", "pgrep", "pkill", "pidof", "watch", "sysctl", "uptime", "w"},
	"psmisc":      {"killall", "fuser", "pstree", "peekfd"},
	"util-linux":  {"mount", "umount", "fdisk", "mkfs", "fsck", "lsblk", "blkid", "findmnt", "losetup", "swapon", "swapoff"},
	"shadow": {"useradd", "userdel", "usermod", "groupadd", "groupdel", "groupmod", "passwd", "chpasswd"},
	// Note: coreutils also provides "stty" and "tty" (defined above in coreutils entry)

	// Scripting languages
	"python3":     {"python3", "python"},
	"python-3.11": {"python3.11"},
	"python-3.12": {"python3.12"},
	"python-3.13": {"python3.13"},
	"perl":        {"perl"},
	"ruby":        {"ruby", "irb", "gem"},
	"nodejs":      {"node", "npm", "npx"},

	// Development tools
	"git":       {"git"},
	"make":      {"make"},
	"cmake":     {"cmake", "ctest", "cpack"},
	"gcc":       {"gcc", "g++", "cpp"},
	"clang":     {"clang", "clang++"},
	"go":        {"go", "gofmt"},

	// JSON/YAML processing
	"jq":  {"jq"},
	"yq":  {"yq"},

	// Database clients
	"postgresql-client":   {"psql", "pg_dump", "pg_restore", "pg_isready"},
	"postgresql-16-client": {"psql", "pg_dump", "pg_restore", "pg_isready"},
	"mysql-client":        {"mysql", "mysqldump", "mysqladmin"},
	"mariadb-client":      {"mysql", "mariadb", "mysqldump", "mariadb-dump"},
	"redis":               {"redis-cli", "redis-server", "redis-benchmark"},
	"valkey":              {"valkey-cli", "valkey-server", "valkey-benchmark"},
	"valkey-cli":          {"valkey-cli"},

	// Container/K8s tools
	"kubectl":     {"kubectl"},
	"helm":        {"helm"},
	"docker-cli":  {"docker"},
	"podman":      {"podman"},
	"skopeo":      {"skopeo"},
	"crane":       {"crane", "gcrane"},

	// AWS/Cloud CLIs
	"aws-cli":    {"aws"},
	"aws-cli-v2": {"aws"},
	"gcloud":     {"gcloud", "gsutil", "bq"},
	"azure-cli":  {"az"},

	// Misc utilities
	"file":       {"file"},
	"findutils":  {"find", "xargs", "locate", "updatedb"},
	"which":      {"which"},
	"tree":       {"tree"},
	"less":       {"less"},
	"vim":        {"vim", "vi"},
	"nano":       {"nano"},
	"openssl":    {"openssl"},
	"ca-certificates": {},

	// POSIX utilities that are shell builtins or provided by multiple packages
	"posix-libc-utils": {"getent", "iconv", "locale", "localedef"},
}

// ResolveCommands takes a list of package names and returns the set of
// commands that would be available if those packages were installed.
func ResolveCommands(packages []string) map[string]bool {
	available := make(map[string]bool)
	for _, pkg := range packages {
		if cmds, ok := PackageProvides[pkg]; ok {
			for _, cmd := range cmds {
				available[cmd] = true
			}
		}
	}
	return available
}

// FindMissingCommands compares required commands against available packages
// and returns commands that are not provided by any of the packages.
func FindMissingCommands(required []string, packages []string) []string {
	available := ResolveCommands(packages)
	var missing []string
	for _, cmd := range required {
		if !available[cmd] {
			missing = append(missing, cmd)
		}
	}
	return missing
}
