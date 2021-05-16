package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Terry-Mao/goim/internal/comet"
	"github.com/Terry-Mao/goim/internal/comet/conf"
	"github.com/Terry-Mao/goim/internal/comet/grpc"
	md "github.com/Terry-Mao/goim/internal/logic/model"
	"github.com/Terry-Mao/goim/pkg/ip"
	"github.com/bilibili/discovery/naming"
	resolver "github.com/bilibili/discovery/naming/grpc"
	log "github.com/golang/glog"
)

const (
	ver   = "2.0.0"
	appid = "goim.comet"
)

func main() {

	flag.Parse()
	if err := conf.Init(); err != nil {
		panic(err)
	}
	rand.Seed(time.Now().UTC().UnixNano())
	runtime.GOMAXPROCS(runtime.NumCPU())
	println(conf.Conf.Debug)
	log.Infof("goim-comet [version: %s env: %+v] start", ver, conf.Conf.Env)

	//1. register discovery
	dis := naming.New(conf.Conf.Discovery)
	resolver.Register(dis)

	//2. 创建一个新的comet-server实体,该实体包含了一个comet的id值、通道等信息; 该实体具体初始化内容有：
/*	2.1.这其中有初始化comet(rpc-client)通道，该通道用来 和logic(grpc-server)通信；
	2.通信内容：comet-rpcClient将新用户连接信息channnel(session)转发给logic;
	3.并且接收logic回复的关于该连接的room，key,hearbeat等信息
	send: 将用户client发来的信令(会话信息，还有什么，待确认和补充),转发给logic处理；todo

	4.todo：heartbeat机制和流程是怎样的？TBD
	*/

	//srv即comet-server，它代表一个comet实体，该实体包含了一个comet的id值、通道等信息
	srv := comet.NewServer(conf.Conf)
	if err := comet.InitWhitelist(conf.Conf.Whitelist); err != nil {
		panic(err)
	}


	/* 2.1. 初始化tcp、Websocket通道，用来和 用户client通信,数据交换*/
	/*
	 *	tcp-server init,该连接通道的作用是：
	 *	recv：client发来的什么消息（待确认和补充）；todo
	 *	send：下推msg 到client;
	 */

	//todo:这里Bind(内容为：[":3101"] )没有给ip地址，这是为啥？
	if err := comet.InitTCP(srv, conf.Conf.TCP.Bind, runtime.NumCPU()); err != nil {
		panic(err)
	}
	if err := comet.InitWebsocket(srv, conf.Conf.Websocket.Bind, runtime.NumCPU()); err != nil {
		panic(err)
	}
	if conf.Conf.Websocket.TLSOpen {
		if err := comet.InitWebsocketWithTLS(srv, conf.Conf.Websocket.TLSBind, conf.Conf.Websocket.CertFile, conf.Conf.Websocket.PrivateFile, runtime.NumCPU()); err != nil {
			panic(err)
		}
	}

	//3.初始化comet-RPCserver通道，用来与job(RPCclient)通信：接收job下推过来的msg
	/*
	该通道的作用：
	recv（作为grpc server）: 接收job下推的msg
	*/
	/*todo: grpc的通信机制和流程是怎样的？？？ 确认和写出来,记录在笔记里，方便以后查阅和优化*/
	rpcSrv := grpc.New(conf.Conf.RPCServer, srv)

	//4. 最后将该comet实体(srv) 注册到服务发现系统dis去
	cancel := register(dis, srv)

	//5. signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	for {
		s := <-c
		log.Infof("goim-comet get a signal %s", s.String())
		switch s {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			if cancel != nil {
				cancel()
			}
			rpcSrv.GracefulStop()
			srv.Close()
			log.Infof("goim-comet [version: %s] exit", ver)
			log.Flush()
			return
		case syscall.SIGHUP:
		default:
			return
		}
	}
}

func register(dis *naming.Discovery, srv *comet.Server) context.CancelFunc {
	env := conf.Conf.Env
	addr := ip.InternalIP()  //comet服务器的内网地址？？
	_, port, _ := net.SplitHostPort(conf.Conf.RPCServer.Addr)
	ins := &naming.Instance{  //表示一个服务实例：comet服务，将其封装成 服务发现类型naming
		Region:   env.Region,   //comet服务所在地区，eg:sh 上海
		Zone:     env.Zone,		//所在机房：sh001
		Env:      env.DeployEnv, //环境信息：(例如：fat1,uat ,pre ,prod)分别对应fat环境 集成环境，预发布和线上
		Hostname: env.Host,  //comet服务主机名称：eg: hostname=bogon
		AppID:    appid, 	 //comet服务实例名词："goim.comet"  服务唯一标识
		Addrs: []string{	 // comet服务地址：grpc://127.0.0.1:8888； 给服务调用者使用
			"grpc://" + addr + ":" + port,
		},
		Metadata: map[string]string{
			md.MetaWeight:  strconv.FormatInt(env.Weight, 10),
			md.MetaOffline: strconv.FormatBool(env.Offline),
			md.MetaAddrs:   strings.Join(env.Addrs, ","), //Addrs: comet服务器的公网地址？
		},
	}
	cancel, err := dis.Register(ins)
	if err != nil {
		panic(err)
	}
	// renew discovery metadata
	go func() {
		for {
			var (
				err   error
				conns int
				ips   = make(map[string]struct{})
			)
			for _, bucket := range srv.Buckets() {
				for ip := range bucket.IPCount() {
					ips[ip] = struct{}{}
				}
				conns += bucket.ChannelCount()
			}
			ins.Metadata[md.MetaConnCount] = fmt.Sprint(conns)
			ins.Metadata[md.MetaIPCount] = fmt.Sprint(len(ips))
			if err = dis.Set(ins); err != nil {
				log.Errorf("dis.Set(%+v) error(%v)", ins, err)
				time.Sleep(time.Second)
				continue
			}
			time.Sleep(time.Second * 10)
		}
	}()
	return cancel
}

