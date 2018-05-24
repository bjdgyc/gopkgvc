package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"
	"log"
)

var (
	configFile = flag.String("c", "./config.json", "The config file")
)

var httpServer = &http.Server{
	ReadTimeout:  30 * time.Second,
	WriteTimeout: 30 * time.Second,
}

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

type Config struct {
	//监听地址
	Addr string `json:"addr"`
	//gopkg服务地址
	GopkgUrl string `json:"gopkg_url"`
	//版本控制服务器地址
	VCSUrl string `json:"vcs_url"`
	//用户名
	VCSAuthUser string `json:"vcs_auth_user"`
	//密码
	VCSAuthPass string `json:"vcs_auth_pass"`
	//是否需要授权验证
	vcsNeedAuth bool
	//gopkg域名
	GopkgHost string
	//http协议
	GopkgScheme string
	VCSHost     string
}

var config = Config{}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()
	data, err := ioutil.ReadFile(*configFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(data, &config)
	if err != nil {
		panic(err)
	}

	parseUrl, err := url.Parse(config.GopkgUrl)
	if err != nil {
		panic(err)
	}

	config.GopkgHost = parseUrl.Host
	config.GopkgScheme = parseUrl.Scheme

	pu, _ := url.Parse(config.VCSUrl)
	config.VCSHost = pu.Host

	if config.VCSAuthUser != "" && config.VCSAuthPass != "" {
		config.vcsNeedAuth = true
	}

	http.HandleFunc("/", handler)
	httpServer.Addr = config.Addr

	fmt.Println("http start " + config.Addr)
	err = httpServer.ListenAndServe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
