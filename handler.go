package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var gogetTemplate = template.Must(template.New("").Parse(`
<html>
<head>
<meta charset="utf-8">
<meta name="go-import" content="{{.GopkgRoot}} git {{.GopkgScheme}}://{{.GopkgRoot}}">
{{$root := .VCSRoot}}{{$tree := .VCSTree}}
<meta name="go-source" content="{{.GopkgRoot}} _ {{.GopkgScheme}}://{{$root}}/tree/{{$tree}}{/dir}
 {{.GopkgScheme}}://{{$root}}/blob/{{$tree}}{/dir}/{file}#L{line}">
</head>
<body>
go get {{.GopkgPath}}
</body>
</html>
`))

//  /platform_base/cconfig_sdk.v2/base
var patternNew = regexp.MustCompile(`^/([a-zA-Z0-9][-a-zA-Z0-9_]*)/([a-zA-Z0-9][-.a-zA-Z0-9_]*)\.((?:v0|v[1-9][0-9]*)(?:\.0|\.[1-9][0-9]*){0,2}(?:-unstable)?)(?:\.git)?((?:/[a-zA-Z0-9][-.a-zA-Z0-9_]*)*)$`)

func handler(resp http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/health-check" {
		resp.Write([]byte("ok"))
		return
	}

	log.Printf("%s requested %s", req.RemoteAddr, req.URL)

	if req.URL.Path == "/" {
		resp.Header().Set("Location", config.VCSUrl)
		resp.WriteHeader(http.StatusTemporaryRedirect)
		return
	}

	m := patternNew.FindStringSubmatch(req.URL.Path)

	//TODO 测试代码
	//sendNotFound(resp, fmt.Sprintln(m))
	//return

	if m == nil {
		sendErrMsg(resp, http.StatusNotFound, "Unsupported URL pattern; see the documentation at gopkg.in for details.")
		return
	}

	if strings.Contains(m[3], ".") {
		sendErrMsg(resp, http.StatusOK, "Import paths take the major version only (.%s instead of .%s); see docs at gopkg.in for the reasoning.",
			m[3][:strings.Index(m[3], ".")], m[3])
		return
	}

	repo := &Repo{
		User:        m[1],
		Name:        m[2],
		SubPath:     m[4],
		FullVersion: InvalidVersion,
	}

	var ok bool
	repo.MajorVersion, ok = parseVersion(m[3])
	if !ok {
		sendErrMsg(resp, http.StatusNotFound, "Version %q improperly considered invalid; please warn the service maintainers.", m[3])
		return
	}

	var changed []byte
	var versions VersionList
	original, err := fetchRefs(repo)
	if err == nil {
		changed, versions, err = changeRefs(original, repo.MajorVersion)
		repo.SetVersions(versions)
	}

	switch err {
	case nil:
		// all ok
	case ErrNoRepo:
		sendErrMsg(resp, http.StatusNotFound, "GitHub repository not found at https://%s", repo.VCSRoot())
		return
	case ErrNoVersion:
		major := repo.MajorVersion
		suffix := ""
		if major.Unstable {
			major.Unstable = false
			suffix = unstableSuffix
		}
		v := major.String()
		sendErrMsg(resp, http.StatusNotFound, `GitHub repository at https://%s has no branch or tag "%s%s", "%s.N%s" or "%s.N.M%s"`,
			repo.VCSRoot(), v, suffix, v, suffix, v, suffix)
		return
	default:
		sendErrMsg(resp, http.StatusBadGateway, "Cannot obtain refs from GitHub: %v", err)
		return
	}

	//程序中转远程仓库数据
	if repo.SubPath == "/git-upload-pack" {
		//请求远端gilab
		url := fmt.Sprintf("%s.git%s", repo.VCSRoot(), "/git-upload-pack")
		reqRemote, err := http.NewRequest("POST", url, req.Body)
		if err != nil {
			sendErrMsg(resp, http.StatusBadGateway, "GitLab get url is error: %v", err)
			return
		}
		reqRemote.Header.Set("User-Agent", req.UserAgent())
		reqRemote.Header.Set("Accept-Encoding", req.Header.Get("Accept-Encoding"))
		reqRemote.Header.Set("Accept", req.Header.Get("Accept"))
		reqRemote.Header.Set("Content-Type", req.Header.Get("Content-Type"))
		if config.vcsNeedAuth {
			reqRemote.SetBasicAuth(config.VCSAuthUser, config.VCSAuthPass)
		}
		respRemote, err := httpClient.Do(reqRemote)
		if err != nil {
			sendErrMsg(resp, http.StatusBadGateway, "GitLab get git-upload-pack error: %v", err)
			return
		}
		defer respRemote.Body.Close()
		if respRemote.StatusCode != http.StatusOK {
			sendErrMsg(resp, http.StatusBadGateway, "GitLab get data error:http_code %d", respRemote.StatusCode)
			return
		}
		//数据写给客户端
		resp.Header().Set("Cache-Control", "no-cache")
		resp.Header().Set("Content-Type", respRemote.Header.Get("Content-Type"))
		resp.Header().Set("X-Accel-Buffering", respRemote.Header.Get("X-Accel-Buffering"))

		_, err = io.Copy(resp, respRemote.Body)
		if err != nil {
			sendErrMsg(resp, http.StatusBadGateway, "GitLab write data error: %v", err)
			return
		}

		return
	}

	if repo.SubPath == "/info/refs" {
		resp.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		resp.Write(changed)
		return
	}

	resp.Header().Set("Content-Type", "text/html")
	if req.FormValue("go-get") == "1" {
		// execute simple template when this is a go-get request
		err = gogetTemplate.Execute(resp, repo)
		if err != nil {
			log.Printf("error executing go get template: %s\n", err)
		}
		return
	}

	renderPackagePage(resp, req, repo)
}

// Repo represents a source code repository on GitHub.
type Repo struct {
	User         string
	Name         string
	SubPath      string
	MajorVersion Version

	// FullVersion is the best version in AllVersions that matches MajorVersion.
	// It defaults to InvalidVersion if there are no matches.
	FullVersion Version

	// AllVersions holds all versions currently available in the repository,
	// either coming from branch names or from tag names. Version zero (v0)
	// is only present in the list if it really exists in the repository.
	AllVersions VersionList
}

// SetVersions records in the relevant fields the details about which
// package versions are available in the repository.
func (repo *Repo) SetVersions(all []Version) {
	repo.AllVersions = all
	for _, v := range repo.AllVersions {
		if v.Major == repo.MajorVersion.Major && v.Unstable == repo.MajorVersion.Unstable && repo.FullVersion.Less(v) {
			repo.FullVersion = v
		}
	}
}

// VCSRoot returns the repository root at VCS, with a schema.
func (repo *Repo) VCSRoot() string {
	return config.VCSUrl + "/" + repo.User + "/" + repo.Name

}

// VCSTree returns the repository tree name at VCS for the selected version.
func (repo *Repo) VCSTree() string {
	if repo.FullVersion == InvalidVersion {
		return "master"
	}
	return repo.FullVersion.String()
}

func (repo *Repo) GopkgScheme() string {
	return config.GopkgScheme
}

// GopkgRoot returns the package root at gopkg.in, without a schema.
func (repo *Repo) GopkgRoot() string {
	return repo.GopkgVersionRoot(repo.MajorVersion)
}

// GopkgPath returns the package path at gopkg.in, without a schema.
func (repo *Repo) GopkgPath() string {
	return repo.GopkgVersionRoot(repo.MajorVersion) + repo.SubPath
}

// GopkgVersionRoot returns the package root in gopkg.in for the
// provided version, without a schema.
func (repo *Repo) GopkgVersionRoot(version Version) string {
	version.Minor = -1
	version.Patch = -1
	v := version.String()

	return config.GopkgHost + "/" + repo.User + "/" + repo.Name + "." + v

}

func sendErrMsg(resp http.ResponseWriter, httpCode int, msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	resp.WriteHeader(httpCode)
	resp.Write([]byte(msg))
}

const refsSuffix = ".git/info/refs?service=git-upload-pack"

var ErrNoRepo = errors.New("repository not found in GitHub")
var ErrNoVersion = errors.New("version reference not found in GitHub")

func fetchRefs(repo *Repo) (data []byte, err error) {
	req, err := http.NewRequest("GET", repo.VCSRoot()+refsSuffix, nil)
	if err != nil {
		return
	}
	if config.vcsNeedAuth {
		req.SetBasicAuth(config.VCSAuthUser, config.VCSAuthPass)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot talk to GitHub: %v", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		// ok
	case 401, 404:
		return nil, ErrNoRepo
	default:
		return nil, fmt.Errorf("error from GitHub: %v", resp.Status)
	}

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading from GitHub: %v", err)
	}
	return data, err
}

func changeRefs(data []byte, major Version) (changed []byte, versions VersionList, err error) {
	var hlinei, hlinej int // HEAD reference line start/end
	var mlinei, mlinej int // master reference line start/end
	var vrefhash string
	var vrefname string
	var vrefv = InvalidVersion

	// Record all available versions, the locations of the master and HEAD lines,
	// and details of the best reference satisfying the requested major version.
	versions = make([]Version, 0)
	sdata := string(data)
	for i, j := 0, 0; i < len(data); i = j {
		size, err := strconv.ParseInt(sdata[i:i+4], 16, 32)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse refs line size: %s", string(data[i:i+4]))
		}
		if size == 0 {
			size = 4
		}
		j = i + int(size)
		if j > len(sdata) {
			return nil, nil, fmt.Errorf("incomplete refs data received from GitHub")
		}
		if sdata[0] == '#' {
			continue
		}

		hashi := i + 4
		hashj := strings.IndexByte(sdata[hashi:j], ' ')
		if hashj < 0 || hashj != 40 {
			continue
		}
		hashj += hashi

		namei := hashj + 1
		namej := strings.IndexAny(sdata[namei:j], "\n\x00")
		if namej < 0 {
			namej = j
		} else {
			namej += namei
		}

		name := sdata[namei:namej]

		if name == "HEAD" {
			hlinei = i
			hlinej = j
		}
		if name == "refs/heads/master" {
			mlinei = i
			mlinej = j
		}

		if strings.HasPrefix(name, "refs/heads/v") || strings.HasPrefix(name, "refs/tags/v") {
			if strings.HasSuffix(name, "^{}") {
				// Annotated tag is peeled off and overrides the same version just parsed.
				name = name[:len(name)-3]
			}
			v, ok := parseVersion(name[strings.IndexByte(name, 'v'):])
			if ok && major.Contains(v) && (v == vrefv || !vrefv.IsValid() || vrefv.Less(v)) {
				vrefv = v
				vrefhash = sdata[hashi:hashj]
				vrefname = name
			}
			if ok {
				versions = append(versions, v)
			}
		}
	}

	// If there were absolutely no versions, and v0 was requested, accept the master as-is.
	if len(versions) == 0 && major == (Version{0, -1, -1, false}) {
		return data, nil, nil
	}

	// If the file has no HEAD line or the version was not found, report as unavailable.
	if hlinei == 0 || vrefhash == "" {
		return nil, nil, ErrNoVersion
	}

	var buf bytes.Buffer
	buf.Grow(len(data) + 256)

	// Copy the header as-is.
	buf.Write(data[:hlinei])

	// Extract the original capabilities.
	caps := ""
	if i := strings.Index(sdata[hlinei:hlinej], "\x00"); i > 0 {
		caps = strings.Replace(sdata[hlinei+i+1:hlinej-1], "symref=", "oldref=", -1)
	}

	// Insert the HEAD reference line with the right hash and a proper symref capability.
	var line string
	if strings.HasPrefix(vrefname, "refs/heads/") {
		if caps == "" {
			line = fmt.Sprintf("%s HEAD\x00symref=HEAD:%s\n", vrefhash, vrefname)
		} else {
			line = fmt.Sprintf("%s HEAD\x00symref=HEAD:%s %s\n", vrefhash, vrefname, caps)
		}
	} else {
		if caps == "" {
			line = fmt.Sprintf("%s HEAD\n", vrefhash)
		} else {
			line = fmt.Sprintf("%s HEAD\x00%s\n", vrefhash, caps)
		}
	}
	fmt.Fprintf(&buf, "%04x%s", 4+len(line), line)

	// Insert the master reference line.
	line = fmt.Sprintf("%s refs/heads/master\n", vrefhash)
	fmt.Fprintf(&buf, "%04x%s", 4+len(line), line)

	// Append the rest, dropping the original master line if necessary.
	if mlinei > 0 {
		buf.Write(data[hlinej:mlinei])
		buf.Write(data[mlinej:])
	} else {
		buf.Write(data[hlinej:])
	}

	return buf.Bytes(), versions, nil
}
