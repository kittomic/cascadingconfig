package cascadingconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// refForm is the syntax of a remote reference (DL-197).
type refForm int

const (
	formURL    refForm = iota // scheme://…
	formGetter                // name::<url>//<path>[?ref=<ref>]
	formImage                 // oci::<registry>/<repository>[:<tag>][@<digest>][//<path>]
)

// remoteRef is a remote reference, parsed and normalised before any policy sees it (DL-197, DL-200, DL-201).
type remoteRef struct {
	ref    IncludeRef // Raw, Scheme, Ref, Location, Path and Version
	form   refForm
	pinned bool
	sum    string // the hex SHA-256 of an integrity fragment
	file   string // the file path the format and the Extension and FileName matchers see: the URL path or inner path
}

var (
	hex64       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitID    = regexp.MustCompile(`^(?i:[0-9a-f]{40}|[0-9a-f]{64})$`)
	ociRepo     = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)
	ociTag      = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	ociDigest   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	userName    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	dnsHost     = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*$`)
	ociHost     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]+)?$`)
	defaultPort = map[string]string{"http": "80", "https": "443", "ssh": "22"}
)

// parseRemote parses raw, a reference with the remote scheme scheme, into its normalised form. name is the app's name,
// for the image form's default path.
func parseRemote(raw, scheme, name string) (remoteRef, error) {
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return remoteRef{}, errors.New("it contains a control character")
		}
	}
	rr := remoteRef{ref: IncludeRef{Raw: raw, Scheme: scheme}}
	var err error
	switch {
	case !hasPrefixFold(raw, scheme+"::"):
		err = rr.parseURL(raw)
	case scheme == "http" || scheme == "https":
		err = fmt.Errorf("%s:: isn't a form; write a %s:// URL", scheme, scheme)
	case scheme == "oci":
		err = rr.parseImage(raw[len("oci::"):], name)
	default:
		err = rr.parseGetter(raw[len(scheme)+2:])
	}
	return rr, err
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// parseURL parses the URL form: the only fragment allowed is an integrity fragment, which is split off as a pin.
func (rr *remoteRef) parseURL(raw string) error {
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		frag := raw[i+1:]
		sum, ok := strings.CutPrefix(frag, "sha256=")
		if !ok || !hex64.MatchString(strings.ToLower(sum)) {
			return errors.New("the only fragment allowed is an integrity fragment, #sha256=<64 hex digits>")
		}
		rr.sum, rr.pinned = strings.ToLower(sum), true
		rr.ref.Version = "sha256:" + rr.sum
		raw = raw[:i]
	}
	u, err := normaliseURL(raw)
	if err != nil {
		return err
	}
	rr.form = formURL
	rr.ref.Ref, rr.ref.Location = u.norm, u.norm
	rr.file = u.path
	return nil
}

// parseGetter parses the getter form's part after name::.
func (rr *remoteRef) parseGetter(rest string) error {
	j := strings.Index(rest, "://")
	if j < 0 || !isScheme(rest[:j]) {
		return errors.New("the getter form needs a scheme:// URL, as in git::https://host/repo.git//path?ref=v1")
	}
	auth := j + 3
	p := strings.IndexByte(rest[auth:], '/')
	if p < 0 {
		return errors.New("the getter form needs a path: //<path> after the URL")
	}
	k := strings.Index(rest[auth+p:], "//")
	if k < 0 {
		return errors.New("the getter form needs a path: //<path> after the URL")
	}
	repo, inner := rest[:auth+p+k], rest[auth+p+k+2:]
	if strings.ContainsAny(repo, "?#") {
		return errors.New("the repository URL can't have a query or a fragment")
	}
	inner, query, _ := strings.Cut(inner, "?")
	var gitRef string
	if query != "" {
		for kv := range strings.SplitSeq(query, "&") {
			k, v, _ := strings.Cut(kv, "=")
			if k != "ref" {
				return fmt.Errorf("unknown parameter %q; only ref is allowed", k)
			}
			if gitRef != "" {
				return errors.New("more than one ref")
			}
			uv, err := url.QueryUnescape(v)
			if err != nil {
				return fmt.Errorf("the ref: %w", err)
			}
			if err := checkRefName(uv); err != nil {
				return err
			}
			gitRef = uv
		}
	}
	u, err := normaliseURL(repo)
	if err != nil {
		return err
	}
	// A trailing / names the same repository, and would run into the // before the path.
	for strings.HasSuffix(u.norm, "/") && len(u.norm) > len(u.scheme)+len("://") {
		u.norm = u.norm[:len(u.norm)-1]
	}
	if inner, err = innerPath(inner); err != nil {
		return err
	}
	rr.form = formGetter
	rr.ref.Location, rr.ref.Path, rr.ref.Version = u.norm, inner, gitRef
	rr.ref.Ref = rr.ref.Scheme + "::" + u.norm + "//" + inner
	if gitRef != "" {
		rr.ref.Ref += "?ref=" + gitRef
	}
	rr.pinned = commitID.MatchString(gitRef)
	rr.file = inner
	return nil
}

// checkRefName applies git's ref-name rules to a getter-form ref, plus &, #, % and +, which the reference's own
// syntax uses.
func checkRefName(ref string) error {
	switch {
	case ref == "":
		return errors.New("an empty ref")
	case strings.HasPrefix(ref, "-"):
		return fmt.Errorf("the ref %q starts with -", ref)
	case strings.Contains(ref, ".."), strings.Contains(ref, "@{"):
		return fmt.Errorf("the ref %q contains .. or @{", ref)
	case strings.ContainsAny(ref, " \t\n\r~^:?*[\\&#%+"),
		strings.ContainsFunc(ref, func(r rune) bool { return r < 0x20 || r == 0x7f || r == utf8.RuneError }):
		return fmt.Errorf("the ref %q contains a character refs can't hold", ref)
	}
	return nil
}

// parseImage parses the image form's part after oci::.
func (rr *remoteRef) parseImage(rest, name string) error {
	image, inner, hasInner := strings.Cut(rest, "//")
	registry, repo, ok := strings.Cut(image, "/")
	if !ok {
		return errors.New("the image reference needs a registry and a repository, as in oci::ghcr.io/acme/config:1")
	}
	registry = strings.ToLower(registry)
	if (!strings.ContainsAny(registry, ".:") && registry != "localhost") || !ociHost.MatchString(registry) {
		return fmt.Errorf("the registry %q must be a host name with a dot or a port, or localhost", registry)
	}
	repo, digest, hasDigest := strings.Cut(repo, "@")
	if hasDigest && !ociDigest.MatchString(digest) {
		return fmt.Errorf("the digest %q must be sha256:<64 hex digits>", digest)
	}
	var tag string
	if i := strings.LastIndexByte(repo, ':'); i >= 0 && !strings.Contains(repo[i:], "/") {
		repo, tag = repo[:i], repo[i+1:]
		if !ociTag.MatchString(tag) {
			return fmt.Errorf("the tag %q isn't valid", tag)
		}
	}
	if !ociRepo.MatchString(repo) {
		return fmt.Errorf("the repository %q isn't valid: lowercase letters, digits and separators", repo)
	}
	if tag == "" && !hasDigest {
		tag = "latest"
	}
	if !hasInner {
		inner = name + ".toml"
	}
	inner, err := innerPath(inner)
	if err != nil {
		return err
	}
	var version, suffix string
	switch {
	case tag != "" && hasDigest:
		version, suffix = tag+"@"+digest, ":"+tag+"@"+digest
	case hasDigest:
		version, suffix = digest, "@"+digest
	default:
		version, suffix = tag, ":"+tag
	}
	rr.form = formImage
	rr.ref.Location, rr.ref.Path, rr.ref.Version = registry+"/"+repo, inner, version
	rr.ref.Ref = "oci::" + registry + "/" + repo + suffix + "//" + inner
	rr.pinned = hasDigest
	rr.file = inner
	return nil
}

// innerPath cleans the path inside a repo or artifact, which must then be a valid fs.FS path: no leading /, no ..
// segment (design.md Traversal protection, step 1).
func innerPath(p string) (string, error) {
	if strings.Contains(p, `\`) {
		return "", fmt.Errorf("the inner path %q contains \\", p)
	}
	if l := strings.ToLower(p); strings.Contains(l, "%2e") || strings.Contains(l, "%2f") || strings.Contains(l, "%5c") {
		return "", fmt.Errorf("the inner path %q has a percent-encoded ., / or \\", p)
	}
	if strings.Contains(p, "#") {
		return "", fmt.Errorf("the inner path %q contains #", p)
	}
	if p == "" {
		return "", errors.New("an empty inner path")
	}
	clean := path.Clean(p)
	if !fs.ValidPath(clean) || clean == "." {
		return "", fmt.Errorf("the inner path %q leaves the repository or artifact", p)
	}
	return clean, nil
}

// normURL is a normalised URL.
type normURL struct {
	norm   string // the normalised string
	scheme string
	path   string // the decoded path
}

// normaliseURL validates and normalises a scheme://… URL (DL-200).
func normaliseURL(raw string) (normURL, error) {
	if strings.Contains(raw, `\`) {
		return normURL{}, errors.New(`it contains \`)
	}
	if strings.Contains(raw, "#") {
		return normURL{}, errors.New("it has a fragment")
	}
	j := strings.Index(raw, "://")
	if j < 0 || !isScheme(raw[:j]) {
		return normURL{}, errors.New("it isn't a scheme:// URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return normURL{}, err
	}
	scheme := strings.ToLower(u.Scheme)
	var user string
	if u.User != nil {
		_, hasPw := u.User.Password()
		if scheme != "ssh" || hasPw || u.User.Username() == "" {
			return normURL{}, errors.New("it has userinfo; only an ssh URL may name a user, without a password")
		}
		if !userName.MatchString(u.User.Username()) {
			return normURL{}, errors.New("the user name may only hold letters, digits, ., _ and -")
		}
		user = u.User.Username() + "@"
	}
	host, err := normaliseHost(u, scheme)
	if err != nil {
		return normURL{}, err
	}
	p, err := normalisePath(u.EscapedPath())
	if err != nil {
		return normURL{}, err
	}
	decoded, err := url.PathUnescape(p)
	if err != nil {
		return normURL{}, err
	}
	norm := scheme + "://" + user + host + p
	if u.RawQuery != "" {
		norm += "?" + normaliseEscapes(u.RawQuery)
	}
	return normURL{norm: norm, scheme: scheme, path: decoded}, nil
}

// normaliseHost lowercases the host, drops a trailing dot and the scheme's default port, and rejects non-ASCII
// hosts and bad ports.
func normaliseHost(u *url.URL, scheme string) (string, error) {
	host, port := u.Hostname(), u.Port()
	for i := range len(u.Host) {
		if u.Host[i] >= 0x80 || u.Host[i] == '%' {
			return "", fmt.Errorf("the host %q isn't ASCII; write it in its xn-- form", u.Host)
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch {
	case host == "" && scheme != "file":
		return "", errors.New("it has no host")
	case strings.Contains(host, ":"):
		if a, err := netip.ParseAddr(host); err != nil || !a.Is6() {
			return "", fmt.Errorf("the host %q isn't an IPv6 address", host)
		}
		host = "[" + host + "]"
	case host != "" && !dnsHost.MatchString(host):
		return "", fmt.Errorf("the host %q isn't a host name: labels of letters, digits, - and _, separated by single dots", host)
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("the port %q isn't a port number", port)
		}
		port = strconv.Itoa(n)
	} else if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("it has an empty port")
	}
	if port != "" && port != defaultPort[scheme] {
		host += ":" + port
	}
	return host, nil
}

// normalisePath checks and normalises an escaped URL path: encoded ., / and \ and encoded control characters are
// rejected, escapes are normalised, and dot segments removed (RFC 3986 §5.2.4).
func normalisePath(p string) (string, error) {
	for i := 0; i < len(p); i++ {
		if p[i] != '%' {
			continue
		}
		if i+2 >= len(p) {
			return "", errors.New("it has a broken percent escape")
		}
		b, err := strconv.ParseUint(p[i+1:i+3], 16, 8)
		if err != nil {
			return "", errors.New("it has a broken percent escape")
		}
		switch {
		case b == '.' || b == '/' || b == '\\':
			return "", fmt.Errorf("its path has a percent-encoded %q (%s)", rune(b), p[i:i+3])
		case b < 0x20 || b == 0x7f:
			return "", errors.New("its path has a percent-encoded control character")
		}
	}
	p = removeDotSegments(normaliseEscapes(p))
	if p == "" {
		p = "/"
	}
	return p, nil
}

// normaliseEscapes decodes escapes of unreserved characters and uppercases the others.
func normaliseEscapes(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+2 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			b.WriteByte(s[i])
			continue
		}
		if c := byte(v); unreserved(c) {
			b.WriteByte(c)
		} else {
			b.WriteString("%" + strings.ToUpper(s[i+1:i+3]))
		}
		i += 2
	}
	return b.String()
}

func unreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' ||
		c == '~'
}

// removeDotSegments is RFC 3986 §5.2.4's algorithm.
func removeDotSegments(in string) string {
	var out []string
	segs := strings.Split(in, "/")
	for i, s := range segs {
		last := i == len(segs)-1
		switch s {
		case ".":
			if last {
				out = append(out, "")
			}
		case "..":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, s)
		}
	}
	return strings.Join(out, "/")
}

// joinRemote resolves raw, a plain path in the remote file parent, against parent's origin (DL-204): its URL, or the
// same repo and ref, or the same artifact. It returns the joined reference, to be parsed again.
func joinRemote(parent *remoteRef, raw string) (string, error) {
	if hasPrefixFold(raw, "file:") || isHomePath(raw) {
		return "", errors.New("a remote file can't include a local file")
	}
	if strings.Contains(raw, `\`) {
		return "", errors.New(`it contains \`)
	}
	if parent.form == formURL {
		base, err := url.Parse(parent.ref.Location)
		if err != nil {
			return "", err
		}
		rel, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		return base.ResolveReference(rel).String(), nil
	}
	if strings.HasPrefix(raw, "/") {
		return "", errors.New("a path in a repository or artifact must be relative")
	}
	joined := path.Clean(path.Dir(parent.ref.Path) + "/" + raw)
	if !fs.ValidPath(joined) {
		return "", errors.New("it climbs above the root of its repository or artifact")
	}
	if parent.form == formImage {
		v := parent.ref.Version
		switch {
		case strings.HasPrefix(v, "sha256:"):
			v = "@" + v
		default:
			v = ":" + v
		}
		return "oci::" + parent.ref.Location + v + "//" + joined, nil
	}
	s := parent.ref.Scheme + "::" + parent.ref.Location + "//" + joined
	if parent.ref.Version != "" {
		s += "?ref=" + parent.ref.Version
	}
	return s, nil
}
