# gopkgvc

## Introduction
gopkgvc
go包的版本管理工具，基于 [http://gopkg.in](http://gopkg.in) 开发。
主要用于企业内部包管理。现支持 （github、gitlab）等仓库的版本管理。

# Screenshot
![Screenshot](https://raw.githubusercontent.com/bjdgyc/gopkgvc/master/gopkgvc.png)

## TODO
* 该程序仅实现了 `http` 协议，如需要 `https` 功能，需结合 `nginx` 等代理工具实现。
* 该程序版本控制是基于 项目的 `tag` 或者 `branch` 实现的
* 程序版本应严格按照 `语义化版本` 写法 [http://semver.org/lang/zh-CN/](http://semver.org/lang/zh-CN/)

## Installation

`go get github.com/bjdgyc/gopkgvc`

## Json config

``` json

{
  "addr" : ":8080", //程序监听地址
  "gopkg_url":"http://mygopkg.com", //包管理地址名
  "vcs_url": "http://mygitlab.com", //gitlab等仓库地址
  "vcs_auth_user":"gitlab_user", //gitlab用户名
  "vcs_auth_pass":"gitlab_pass" //gitlab密码
}


```

## Start

`go build && ./gopkgvc -c ./config.json`


## Use
请使用浏览器打开 `http://mygopkg.com/user/project.v1` 根据页面操作即可