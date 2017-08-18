package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
)

const packageTemplateString = `<!DOCTYPE html>
<html >
	<head>
		<meta charset="utf-8">
		<title>{{.Repo.Name}}.{{.Repo.MajorVersion}}{{.Repo.SubPath}} - {{.Repo.GopkgPath}}</title>
		<link href='//fonts.googleapis.com/css?family=Ubuntu+Mono|Ubuntu' rel='stylesheet' >
		<link href="//cdn.bootcss.com/font-awesome/4.0.3/css/font-awesome.min.css" rel="stylesheet" >
		<link href="//cdn.bootcss.com/bootstrap/3.1.1/css/bootstrap.min.css" rel="stylesheet" >
		<style>
			html,
			body {
				height: 100%;
			}

			@media (min-width: 1200px) {
				.container {
					width: 970px;
				}
			}

			body {
				font-family: 'Ubuntu', sans-serif;
			}

			pre {
				font-family: 'Ubuntu Mono', sans-serif;
			}

			.main {
				padding-top: 20px;
			}

			.buttons a {
				width: 100%;
				text-align: left;
				margin-bottom: 5px;
			}

			.getting-started div {
				padding-top: 12px;
			}

			.getting-started p, .synopsis p {
				font-size: 1.3em;
			}

			.getting-started pre {
				font-size: 15px;
			}

			.versions {
				font-size: 1.3em;
			}
			.versions div {
				padding-top: 5px;
			}
			.versions a {
				font-weight: bold;
			}
			.versions a.current {
				color: black;
				font-decoration: none;
			}

			/* wrapper for page content to push down footer */
			#wrap {
				min-height: 100%;
				height: auto !important;
				height: 100%;
				/* negative indent footer by it's height */
				margin: 0 auto -40px;
			}

			/* footer styling */
			#footer {
				height: 40px;
				background-color: #eee;
				padding-top: 8px;
				text-align: center;
			}

			/* footer fixes for mobile devices */
			@media (max-width: 767px) {
				#footer {
					margin-left: -20px;
					margin-right: -20px;
					padding-left: 20px;
					padding-right: 20px;
				}
			}
		</style>
	</head>
	<body>

		<div id="wrap" >
			<div class="container" >
				<div class="row" >
					<div class="col-sm-12" >
						<div class="page-header">
							<h1>{{.Repo.GopkgPath}}</h1>
							{{.Synopsis}}
						</div>
					</div>
				</div>
				{{ if .Repo.MajorVersion.Unstable }}
					<div class="col-sm-12 alert alert-danger">
						This is an <b><i>unstable</i></b> package and should <i>not</i> be used in released code.
					</div>
				{{ end }}
				<div class="row" >
					<div class="col-sm-12" >
						<a class="btn btn-lg btn-info" href="{{.Repo.VCSRoot}}/tree/{{.Repo.VCSTree}}{{.Repo.SubPath}}" ><i class="fa fa-github"></i> Source Code</a>
						<a class="btn btn-lg btn-info" href="{{.Repo.VCSRoot}}/blob/{{.Repo.VCSTree}}/README.md" ><i class="fa fa-info-circle"></i> Documentation</a>
					</div>
				</div>
				<div class="row main" >
					<div class="col-sm-8 info" >
						<div class="getting-started" >
							<h2>Getting started</h2>
							<div>
								<p>To get the package, execute:</p>
								<pre>go get {{if eq .Repo.GopkgScheme "http"}}-insecure {{end}}{{.Repo.GopkgPath}}</pre>
							</div>
							<div>
								<p>To import this package, add the following line to your code:</p>
								<pre>import "{{.Repo.GopkgPath}}"</pre>
								{{if .PackageName}}<p>Refer to it as <i>{{.PackageName}}</i>.{{end}}
							</div>
							<div>
								<p>For more details, see the API documentation.</p>
							</div>
						</div>
					</div>
					<div class="col-sm-3 col-sm-offset-1 versions" >
						<h2>Versions</h2>
						{{ if .LatestVersions }}
							{{ range .LatestVersions }}
								<div>
									<a href="//{{gopkgVersionRoot $.Repo .}}{{$.Repo.SubPath}}" {{if eq .Major $.Repo.MajorVersion.Major}}{{if eq .Unstable $.Repo.MajorVersion.Unstable}}class="current"{{end}}{{end}}>v{{.Major}}{{if .Unstable}}-unstable{{end}}</a>
									&rarr;
									<span class="label label-default">{{.}}</span>
								</div>
							{{ end }}
						{{ else }}
							<div>
								<a href="//{{$.Repo.GopkgPath}}" class="current">v0</a>
								&rarr;
								<span class="label label-default">master</span>
							</div>
						{{ end }}
					</div>
				</div>
			</div>
		</div>

		<div id="footer">
			<div class="container">
				<div class="row">
					<div class="col-sm-12">
						<p class="text-muted credit">Power By <a href="https://github.com/bjdgyc">bjdgyc<a>
						| <a href="https://gopkg.in">gopkg.in<a></p>
					</div>
				</div>
			</div>
		</div>

	</body>
</html>`

var packageTemplate *template.Template

func gopkgVersionRoot(repo *Repo, version Version) string {
	return repo.GopkgVersionRoot(version)
}

var packageFuncs = template.FuncMap{
	"gopkgVersionRoot": gopkgVersionRoot,
}

func init() {
	var err error
	packageTemplate, err = template.New("page").Funcs(packageFuncs).Parse(packageTemplateString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: parsing package template failed: %s\n", err)
		os.Exit(1)
	}
}

type packageData struct {
	Repo           *Repo
	LatestVersions VersionList // Contains only the latest version for each major
	PackageName    string      // Actual package identifier as specified in http://golang.org/ref/spec#PackageClause
	Synopsis       string
	GitTreeName    string
}

func renderPackagePage(resp http.ResponseWriter, req *http.Request, repo *Repo) {
	data := &packageData{
		Repo: repo,
	}

	// Calculate the latest version for each major version, both stable and unstable.
	latestVersions := make(map[int]Version)
	for _, v := range repo.AllVersions {
		if v.Unstable {
			continue
		}
		v2, exists := latestVersions[v.Major]
		if !exists || v2.Less(v) {
			latestVersions[v.Major] = v
		}
	}
	data.LatestVersions = make(VersionList, 0, len(latestVersions))
	for _, v := range latestVersions {
		data.LatestVersions = append(data.LatestVersions, v)
	}
	sort.Sort(sort.Reverse(data.LatestVersions))

	if repo.FullVersion.Unstable {
		// Prepend post-sorting so it shows first.
		data.LatestVersions = append([]Version{repo.FullVersion}, data.LatestVersions...)
	}

	err := packageTemplate.Execute(resp, data)
	if err != nil {
		log.Printf("error executing package page template: %v", err)
	}
}
