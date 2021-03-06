package main

import (
	"./util"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
    "bytes"
    "compress/zlib"
    "encoding/hex"
//    "encoding/binary"
)


type Client struct {
	Name                  string
	Uuid                  string
	RemoteAddr, LocalAddr net.Addr
	Conn, ProxyConn       net.Conn
}

func (connection Client) Say(sender, message string) {
    connection.Message(sender, message, 0x01)
}
func say(sender, message string) {
	for i := 0; i < len(connections); i++ {
		conn := connections[i]
		//conn.Conn.Write(encoded)
		conn.Say(sender, message)
	}
}
func genMsg(sender, msg string) []byte {
    n_message := msg
    length := len(sender) + len(n_message) + 8
    encoded := []byte{0x05, byte(length * 2), 0x03, 0x00, 0x00, 0x00, 0x00, 0x00}
    encoded = append(encoded, byte(len(sender)))
    encoded = append(encoded, []byte(sender)...)
    encoded = append(encoded, byte(len(n_message)))
    encoded = append(encoded, []byte(n_message)...)
    encoded = append(encoded, []byte{0x30, 0x04, 0x98, 0x6d, 0x2b, 0x0c, 0x60, 0x04, 0x02, 0x8d, 0xc2, 0x51}...)
    return encoded
}
func (connection Client) GiveItem(item string, count int){
    length := len(item) + 4
    encoded := []byte{0x14, byte(length * 2)}
    encoded = append(encoded, byte(len(item)))
    encoded = append(encoded, []byte(item)...)
    encoded = append(encoded, byte( count +1 ))
    encoded = append(encoded, []byte{0x07, 0x00, 0x30, 0x06, 0x83, 0xb7, 0x74, 0x2b, 0x1e, 0x81, 0x1e, 0x0c, 0x02, 0x8d, 0xcb, 0x04, 0x04, 0x8b, 0xa3, 0x4e, 0x08, 0x88, 0x80, 0x01}...)
    connection.Conn.Write(encoded)
}
func giveItem(name, item string, count int) (lines []string) {
	for i := 0; i < len(connections); i++ {
		conn := connections[i]
	    if conn.Name == name {
            lines = append(lines, "Giving "+strconv.Itoa(count)+" "+item+" to "+conn.Name+".")
            conn.GiveItem(item, count)
        }
    }
    return
}
/*
Colors: 
    0x00 - Green
    0x01 - Yellow
    0x02 - Brick
    0x03 - Gray
    All others - Light gray
*/
func (connection Client) Message(sender, message string, color byte) {
    if len(sender)>30 {
        sender = sender[0:30]
    }
	for len(message) > 0 {
		end := 50 - len(sender)
		if len(message) < end {
			end = len(message)
		}
		n_message := message[:end]
		if len(message) >= end {
			message = message[end:]
		}
		length := len(sender) + len(n_message) + 8
/*        buf := make([]byte, binary.MaxVarintLen64)
        v := buf[:binary.PutVarint(buf, int64(length))]
        fmt.Println("Bytesss",v,byte(length*2))*/
		// Normal text (what looks like a player)
		//encoded := []byte{0x05, byte(length * 2), 0x01, 0x00, 0x00, 0x00, 0x00, 0x02}
		encoded := []byte{0x05, byte(length * 2), color, 0x00, 0x00, 0x00, 0x00, 0x00}
		encoded = append(encoded, byte(len(sender)))
		encoded = append(encoded, []byte(sender)...)
		encoded = append(encoded, byte(len(n_message)))
		encoded = append(encoded, []byte(n_message)...)
		encoded = append(encoded, []byte{0x30, 0x04, 0x98, 0x6d, 0x2b, 0x0c, 0x60, 0x04, 0x02, 0x8d, 0xc2, 0x51}...)
		connection.Conn.Write(encoded)
		sender = ""
	}
}
func (client Client) MOTD(){
    client.Message("", "MOTD: "+config.MOTD, 0x00)
}
func (connection Client) Console(message string) {
    connection.Message("", message, 0x04)
}
func broadcast(message string, color byte) {
	for i := 0; i < len(connections); i++ {
		conn := connections[i]
		conn.Message("", message, color)
	}
}
func filterConn(dst, src net.Conn) (written int64, err error){
    buf := make([]byte, 32*1024)
    for {
        nr, er := src.Read(buf)
        if nr > 0 {
            // Drops the "No command found" response message from starbound_server
            if buf[0]==0x05 && ( buf[2]==0x03 && buf[3]==0x00 || buf[3]==0x03 && buf[4]==0x00) && bytes.Index(buf,[]byte("No such command"))!=-1 {
                length := 16 + int(buf[15])
                buf = buf[length:]
                nr -= length
            }
            // Admin chat highlighting
            first_bit := buf[2]==0x01 && buf[3]==0x00
            second_bit := buf[3]==0x01 && buf[4]==0x00
            if buf[0]==0x05 && (first_bit || second_bit) {
                index := 8
                if second_bit {
                    index = 9
                }
                length := buf[index]
                name := string(buf[index+1:index+1+int(length)])
                for i:=0;i<len(config.Admins);i++ {
                    if config.Admins[i].Name == name {
                        if first_bit {
                            buf[2]=0x00
                        } else {
                            buf[3]=0x00
                        }
                    }
                }
            }
            nw, ew := dst.Write(buf[0:nr])
            if nw > 0 {
                written += int64(nw)
            }
            if ew != nil {
                err = ew
                break
            }
            if nr != nw {
                err = io.ErrShortWrite
                break
            }
        }
        if er == io.EOF {
            break
        }
        if er != nil {
            err = er
            break
        }
    }
    fmt.Println("[Conn Drop Err]",err)
    return
}
func filterConnCS(dst, src net.Conn, clients chan Client) (written int64, err error){
    buf := make([]byte, 32*1024)
    first := true
    var client Client
    for {
        nr, er := src.Read(buf)
        if nr > 0 {
            if first {
                //fmt.Println("First length:",nr, buf[0:20])
                length := byte(0)
                cur := byte(0)
                i := 1
                for i=1; (cur & 0x80) != 0 || i==1; i++ {
                    cur = buf[i] & 0xff
                    length = length << 7 | (cur & 0x7f)
                    //fmt.Println("L",length)
                }
                //fmt.Println("Length",length)
                //fmt.Println("First bytes",buf[:80])
                i = bytes.Index(buf, []byte{0x78,0x9c})
                fmt.Println("i", i)
                var uncomp []byte
                nocomp := false
                if i != -1 {
                    comp := bytes.NewBuffer(buf[i:])
                    r, err := zlib.NewReader(comp)
                    if err!=nil {
                        fmt.Println("Err:",err)
                    }
                    uncomp = make([]byte,32*1024)
                    r.Read(uncomp)
                    r.Close()
                } else {
                    nocomp = true
                    uncomp = buf[bytes.IndexByte(buf,0x80)+1:]
                    fmt.Println("[Error] Player hasn't first logged into a singleplayer world. No shipdata.")
                }

                asset_digest := uncomp[1:uncomp[0]+1]
                index := uncomp[0]+1
                claim := uncomp[index]
                index += 1
                unk := uncomp[index]
                index += 1
                uuid := hex.EncodeToString(uncomp[index:index+16])
                //fmt.Println("UUID", uuid)
                //fmt.Println("Uncomp",uncomp[index+16:150])
                index += 16
                _ = asset_digest
                _ = claim
                _ = unk
                if !nocomp {
                    index += 1
                }
                name := string(uncomp[index:index+uncomp[index-1]])
                //fmt.Println("Name",name)
                client = Client{name, uuid, src.RemoteAddr(), dst.LocalAddr(), src, dst}
                //client.MOTD()
                fmt.Println("[Client]", client)
                if nocomp {
                    fmt.Println("[Error] "+client.Name+" hasn't first logged into a singleplayer world. No shipdata.")
                }
                clients <- client
                first = false
            }
            nw, ew := dst.Write(buf[0:nr])
            if nw > 0 {
                written += int64(nw)
            }
            if ew != nil {
                err = ew
                break
            }
            if nr != nw {
                err = io.ErrShortWrite
                break
            }
        }
        if er == io.EOF {
            break
        }
        if er != nil {
            err = er
            break
        }
    }
    for i := 0; i < len(connections); i++ {
        if connections[i] == client {
            connections = append(connections[:i], connections[i+1:]...)
            break
        }
    }
    fmt.Println("[Conn Drop Err]",err)
    dst.Close()
    broadcast(client.Name + " has left.", 0x02)
    return
}
func netProxy(connections chan Client) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", config.ProxyAddress)
	checkError(err)
	listener, err := net.ListenTCP("tcp", tcpAddr)
	checkError(err)

	rAddr, err := net.ResolveTCPAddr("tcp", config.ServerAddress)
	if err != nil {
		panic(err)
	}
	for {
		conn, _ := listener.Accept()
		//fmt.Println("Server listerning")
		banned := false
		for i := 0; i < len(config.Bans); i++ {
			ban := config.Bans[i]
			if strings.Index(conn.RemoteAddr().String(), ban.Addr) != -1 {
				fmt.Println("[Info] Banned client tried to connect from IP:", conn.RemoteAddr().String(), "Matched Rule Name:", ban.Name, "Rule IP:", ban.Addr)
				banned = true
				conn.Close()
			}
		}
		if !banned {
			rConn, err := net.DialTCP("tcp", nil, rAddr)
			if err != nil {
				fmt.Println("[Error] Client tried to connect. Server is not accepting connections.")
				conn.Close()
			} else {
				fmt.Println("Remote addr:", rConn.LocalAddr())
				go filterConn(conn, rConn)
				go filterConnCS(rConn, conn, connections)
                //go io.Copy(rConn, conn)
				defer rConn.Close()
			}
			defer conn.Close()
		}
	}
}

type ServerInfo struct {
	Type, Data string
}


func monitorServer(cs chan ServerInfo) {
	for {
		cmd := exec.Command(config.ServerPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Println(err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			fmt.Println(err)
		}
		err = cmd.Start()
		if err != nil {
			fmt.Println(err)
		}
		_ = stderr
		//go io.Copy(os.Stdout, stdout)
		//go io.Copy(os.Stderr, stderr)
		reader := bufio.NewReader(stdout)
		for {
			line, _, err := reader.ReadLine()
			if err != nil {
				fmt.Println(err)
				break
			} else {
				trim := strings.Trim(string(line), "\n \r")
				if strings.Index(trim, "Info: Client ") == 0 {
					cs <- ServerInfo{"client", trim}
				} else if strings.Index(trim, "Info:  <") == 0 {
					cs <- ServerInfo{"chat", trim}
				} else if strings.Index(trim, "Info: TcpServer") == 0 {
					cs <- ServerInfo{"serverup", trim}
				} else if strings.Index(trim, "Info: Shutting down world ") == 0 {
                    parts := strings.Split(strings.TrimRight(trim, " ")," ")
                    cs <- ServerInfo{"worlddown", parts[len(parts)-1]}
                } else if strings.Index(trim, "Info: Loading world db for world ") == 0 || strings.Index(trim, "Info: Creating world ") == 0 {
                    parts := strings.Split(trim," ")
                    cs <- ServerInfo{"worldup", parts[len(parts)-1]}
                }
			}
		}
		cmd.Wait()
		fmt.Println("[Error] Server crashed. Rebooting in 3 seconds...")
		time.Sleep(time.Second * 3)
		fmt.Println("[Error] Rebooting...")
	}
}
func printMessages(count int) (lines []string) {
	if count == 0 {
		count = 20
	}
	path := logFile
	if len(path) == 0 {
		path = config.ServerPath
		if path[len(path)-4:] == ".exe" {
			path = path[:len(path)-4]
		}
		path += ".log"
	}

	logs, err := util.ReadLines(path)
	if err != nil {
		lines = append(lines, "[Error] Failed to read log file: "+err.Error()+"Path: "+path)
	}
	if count > len(logs) {
		count = len(logs)
	}
	lines = append(lines, "Last "+strconv.Itoa(count) + " log messages (of "+strconv.Itoa(len(logs))+"):")
	for i := 0; i < count; i++ {
		line := len(logs) - count + i
		lines = append(lines, logs[line])
		//fmt.Println(lines[line])
	}
	return
}
func banip(ip, desc string) (lines []string) {
	lines = append(lines, "[Banned] ip: "+ip+" Desc: "+desc)
	config.Bans = append(config.Bans, Ban{ip, desc})
	for i := 0; i < len(connections); i++ {
		conn := connections[i]
		addr_bits := strings.Split(conn.RemoteAddr.String(), ":")
		addr := strings.Join(addr_bits[:len(addr_bits)-1], ":")
		if strings.Index(addr, ip) != -1 {
			lines = append(lines, "[Banned] Name: "+conn.Name+" IP: "+addr)
			broadcast(conn.Name + " has been banned.", 0x02)
			conn.Conn.Close()
			conn.ProxyConn.Close()
		}
	}
	writeConfig()
	return
}
func processCommand(command string, args []string, client *Client) (response []string) {
    ingame := client!=nil
	if client != nil {
		for i := 0; i < len(commands); i++ {
			cmd := commands[i]
			if cmd.Command == command && cmd.Auth {
                authed := false
                for i:=0;i<len(config.Admins);i++ {
                    admin := config.Admins[i]
                    if admin.Uuid == client.Uuid {
                        authed = true
                    }
                }
				if authed {
				} else {
					response = append(response, "Not on admin list.")
					response = append(response, printWTF())
					return
				}
			}
		}
	}
	if command == "help" {
        if len(args)==0 {
            response = append(response, genHelp(client != nil)...)
        } else {
            for i := 0; i < len(commands); i++ {
                cmd := commands[i]
                if cmd.Command == args[0] {
                    msg := ""
                    if ingame {
                        msg += "/"
                    }
                    msg += cmd.Command + " "
                    if cmd.Auth && ingame {
                        msg += "<pass> "
                    }
                    response = append(response, msg+cmd.Fields)
                    response = append(response, "  "+cmd.Description)
                }
            }
        }
	} else if command == "color" {
        if len(args)>1 {
            msg := strings.Join(args[1:], " ")
            color, _ := strconv.Atoi(args[0])
            for i:=0; i<len(connections); i++ {
                connections[i].Message("", msg, byte(color))
            }
        } else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
        }
    } else if command == "bans" {
		response = append(response, "[Bans]")
		for i := 0; i < len(config.Bans); i++ {
			conn := config.Bans[i]
			response = append(response, "  Name: "+conn.Name+", IP: "+conn.Addr)
		}
	} else if command == "banip" {
		if len(args) > 0 {
			desc := "None"
			if len(args) > 1 {
				desc = strings.Join(args[1:], " ")
			}
			banip(args[0], desc)
		} else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
		}
	} else if command == "unbanip" {
		if len(args) > 0 {
			//config.Bans = append(config.Bans, Ban{parts[1],"None"})
			for i := 0; i < len(config.Bans); i++ {
				if config.Bans[i].Addr == args[0] {
					conn := config.Bans[i]
					response = append(response, "[Unbanned] Name: ", conn.Name+", IP: "+conn.Addr)
					config.Bans = append(config.Bans[:i], config.Bans[i+1:]...)
					writeConfig()
				} else if strings.Index(config.Bans[i].Addr, args[0]) != -1 {
					response = append(response, "Did you mean: "+config.Bans[i].Name+" instead? (IP: "+config.Bans[i].Addr+")")
				}
			}
		} else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
		}
	} else if command == "ban" {
		if len(args) > 0 {
			name := strings.Join(args, " ")
			for i := 0; i < len(connections); i++ {
				conn := connections[i]
				addr_bits := strings.Split(conn.RemoteAddr.String(), ":")
				addr := strings.Join(addr_bits[:len(addr_bits)-1], ":")
				if conn.Name == name {
					response = append(response, "[Banned] Name: "+conn.Name+", IP: "+addr)
					config.Bans = append(config.Bans, Ban{addr, conn.Name})
					broadcast(conn.Name + " has been banned.", 0x02)
					conn.Conn.Close()
					conn.ProxyConn.Close()
					writeConfig()
				} else if strings.Index(conn.Name, name) != -1 {
					response = append(response, "Did you mean: "+conn.Name+" instead? (IP: "+addr+")")
				}
			}
		} else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
		}
	} else if command == "unban" {
		if len(args) > 0 {
			//config.Bans = append(config.Bans, Ban{parts[1],"None"})
			name := strings.Join(args, " ")
			for i := 0; i < len(config.Bans); i++ {
				if strings.Index(config.Bans[i].Name, name) != -1 {
					conn := config.Bans[i]
					response = append(response, "[Unbanned] Name: "+conn.Name+", IP:"+conn.Addr)
					config.Bans = append(config.Bans[:i], config.Bans[i+1:]...)
					writeConfig()
					break
				}
			}
		} else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
		}
	} else if command == "kick" {
		if len(args) > 0 {
			//config.Bans = append(config.Bans, Ban{parts[1],"None"})
			name := strings.Join(args, " ")
			for i := 0; i < len(connections); i++ {
				conn := connections[i]
				addr_bits := strings.Split(conn.RemoteAddr.String(), ":")
				addr := strings.Join(addr_bits[:len(addr_bits)-1], ":")
				if conn.Name == name {
					response = append(response, "[Kicked] Name: "+conn.Name+", IP: "+addr)
					broadcast(conn.Name + " has been kicked.",0x02)
					conn.Conn.Close()
					conn.ProxyConn.Close()
				} else if strings.Index(conn.Name, name) != -1 {
					response = append(response, "Did you mean: "+conn.Name+" instead? (IP: "+addr+")")
				}
			}
		} else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
		}
	} else if command == "item" {
        len := len(args)
        if len >= 3 {
            count, _ := strconv.Atoi( args[len-1] )
            item := args[len-2]
            nick := strings.Join(args[0:len-2], " ")
            fmt.Println(nick, item, count)
            response = append(response, giveItem(nick, item, count )...)
        } else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
        }
    } else if command == "say" {
		message := strings.Join(args[1:], " ")
		say(args[0], message)
	} else if command == "broadcast" {
		message := strings.Join(args, " ")
		broadcast(message, 0x02)
	} else if command == "clients" {
		response = append(response, "[Clients]")
		for i := 0; i < len(connections); i++ {
			conn := connections[i]
			response = append(response, "  "+conn.Name+" - UUID: "+conn.Uuid+", IP: "+conn.RemoteAddr.String())
		}
	} else if command == "players" {
		response = append(response, "[Players]")
        players := make([]string,0)
		for i := 0; i < len(connections); i++ {
			conn := connections[i]
			players = append(players, conn.Name)
            //response = append(response, "  "+conn.Name+" - UUID: "+conn.Uuid+", IP: "+conn.RemoteAddr.String())
		}
        response = append(response,strings.Join(players,", "))
	} else if command == "worlds" {
		response = append(response, "[Worlds]")
        for i:=0;i<len(worlds);i++ {
            response = append(response, worlds[i])
        }
	} else if command == "admins" {
		response = append(response, "[Admins]")
        admins := make([]string, 0)
        for i:=0;i<len(config.Admins);i++ {
            admins = append(admins,config.Admins[i].Name)
        }
        response = append(response, strings.Join(admins, ", "))
	} else if command == "adminadd" {
        if len(args) > 0 {
            var clientt Client
            found := false
            for i:=0;i<len(connections);i++ {
                if connections[i].Name == strings.Join(args, " ") {
                    found = true
                    clientt = connections[i]
                    break
                }
            }
            if found {
                response = append(response, "Added "+clientt.Name+" to the admin list.")
                config.Admins = append(config.Admins, User{clientt.Name, clientt.Uuid})
                writeConfig()
            } else {
                response = append(response, "Player not found.")
            }
        } else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
        }
    } else if command == "adminrem" {
        if len(args) > 0 {
            for i:=0; i< len(config.Admins); i++ {
                if config.Admins[i].Name == strings.Join(args, " ") {
                    response = append(response, "Removed "+args[0]+" from the admin list.")
                    config.Admins = append(config.Admins[:i], config.Admins[(i+1):]...)
                    writeConfig()
                }
            }
        } else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
        }
    } else if command == "log" {
		count := 20
		if len(args) == 1 {
			count, _ = strconv.Atoi(args[0])
		}
		response = append(response, printMessages(count)...)
	} else if command == "setmotd" {
        if len(args) > 1 {
            config.MOTD = strings.Join(args, " ")
            broadcast("MOTD: "+config.MOTD, 0x00)
            response = append(response, "MOTD: "+config.MOTD)
            writeConfig()
        } else {
			response = append(response, "Invalid syntax.")
			response = append(response, printWTF())
        }
    } else if command == "motd" {
        if ingame {
            client.Message("","MOTD: "+config.MOTD, 0x00)
        } else {
            response = append(response, "MOTD: "+config.MOTD)
        }
    } else {
		if len(command) > 0 {
			response = append(response, "Unknown command: "+command)
		}
		response = append(response, printWTF())
	}
	return
}
func cli() {
	fmt.Println("Starry CLI - Welcome to Starry!")
	fmt.Println("Starry is a command line Starbound and remote access administration tool.")
	printWTF()
	for {
		fmt.Print("> ")
		reader := bufio.NewReader(os.Stdin)
		raw_input, _ := reader.ReadString('\n')
		trimmed := strings.Trim(raw_input, "\n \r")
		parts := strings.Split(trimmed, " ")
		command := parts[0]
		resp := processCommand(command, parts[1:], nil)
		for i := 0; i < len(resp); i++ {
			fmt.Println(resp[i])
		}
	}
}

type Command struct {
	Command, Fields, Description, Category string
	Auth                                   bool
}

var commands []Command

func genHelp(ingame bool) (lines []string) {
	lines = append(lines, "[Commands]")
	categories := make([]string, 0)
	for i := 0; i < len(commands); i++ {
		command := commands[i]
		found := false
		for j := 0; j < len(categories); j++ {
			if categories[j] == command.Category {
				found = true
				break
			}
		}
		if !found {
			categories = append(categories, command.Category)
		}
	}
	for i := 0; i < len(categories); i++ {
		category := categories[i]
		lines = append(lines, "<"+category+">")
		for i := 0; i < len(commands); i++ {
			command := commands[i]
            if command.Category == category {
                msg := "  "
                if ingame {
                    msg += "/"
                }
                msg += command.Command + " "
                lines = append(lines, msg+command.Fields)
                if !ingame {
                    lines = append(lines, "    - "+command.Description)
                }
            }
		}
	}
	return
}
func printWTF() string {
	return "Type 'help' for more information."
}

type Ban struct {
	Addr, Name string
}

var connections []Client

var logFile string

type Config struct {
	ServerPath    string
	LogFile       string
	ServerAddress string
	ProxyAddress  string
	Password      string
    MOTD          string
    FirstConnectItems map[string]int
    Admins        []User
	Bans          []Ban
    Remembered    []string
}
type User struct {
    Name, Uuid string
}
var worlds []string
var config Config
//var admins []string = []string{}
//var motd string = "Welcome to Starry!"
//var password string = "changethis"
//
//var serverAddress string = "localhost:21024"
//var proxyAddress string = "0.0.0.0:21025"
//var serverPath string = "/home/rice/.starbound/linux64/starbound_server"
func writeConfig() {
	b, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		fmt.Println("[Error] Failed to create JSON config.")
	}
	util.WriteLines([]string{string(b)}, "starry.config")
}
func readConfig() {
	lines, err := util.ReadLines("starry.config")
	if err != nil {
		fmt.Println("[Error Reading Config]", err)
		writeConfig()
	} else {
		err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &config)
		if err != nil {
			fmt.Println("[Error]", err)
            config = Config{
                "/home/rice/.starbound/linux64/starbound_server",
                "",
                "localhost:21024",
                "0.0.0.0:21025",
                "changethis",
                "Welcome to Starry!",
                make(map[string]int),
                []User{},
                []Ban{},
                []string{},
            }
		} else {
			/*serverPath = config.ServerPath
			logFile = config.LogFile
			serverAddress = config.ServerAddress
			proxyAddress = config.ProxyAddress
			password = config.Password
            motd = config.MOTD
			bans = config.Bans*/
		}
	}
}

func (client Client) delayWelcome() {
    time.Sleep(time.Second)
    client.MOTD()
    found := false
    for i:=0;i<len(config.Remembered);i++ {
        if config.Remembered[i]==client.Uuid {
            found = true
        }
    }
    if !found {
        for k, v := range config.FirstConnectItems {
            client.GiveItem(k, v)
        }
        config.Remembered = append(config.Remembered, client.Uuid)
        writeConfig()
    }
}

func main() {
	commands = []Command{
		Command{"clients", "", "Connected client information (UUID, IP).", "General", true},
		Command{"players", "", "Show online players", "General", false},
		Command{"say", "<sender name> <message>", "Say something.", "General", true},
		Command{"broadcast", "<message>", "Show grey text in chat.", "General", true},
		Command{"color", "<color> <message>", "Similar to broadcast but with color.", "General", true},
		Command{"help", "[<command>]", "Information on commands.", "General", false},
		Command{"log", "[<count>]", "Last <count> or 20 log messages.", "General", true},
		Command{"nick", "<name>", "Change your character's name. In game only.", "General", false},
		Command{"item", "<name> <item> <count>", "Give items to a player", "General", true},
		Command{"motd", "", "View the MOTD", "General", false},
		Command{"setmotd", "<message>", "Sets the MOTD", "General", true},
		Command{"worlds", "", "List loaded worlds.", "General", false},
		Command{"bans", "", "Show ban list.", "Bans", true},
		Command{"ban", "<name>", "Ban an IP by player name.", "Bans", true},
		Command{"banip", "<ip> [<name>]", "Ban an IP or range (eg. 8.8.8.).", "Bans", true},
		Command{"unban", "<name>", "Unban an IP by name.", "Bans", true},
		Command{"unbanip", "<ip>", "Unban an IP.", "Bans", true},
		Command{"admins", "", "Lists the admins.", "Admin", true},
		Command{"adminadd", "<name>", "Adds a player to the admin list.", "Admin", true},
		Command{"adminrem", "<name>", "Removes a player from the admin list.", "Admin", true},
	}
	readConfig()
	clientChan := make(chan Client)
	go netProxy(clientChan)
	serverChan := make(chan ServerInfo)
	go monitorServer(serverChan)
	go cli()
	for {
		select {
		case info, ok := <-serverChan:
			if !ok {
				fmt.Println("Server Monitor channel closed!")
			} else {
				if info.Type == "client" {
					// Info: Client 'Tom' <1> (127.0.0.1:52029) connected
					// Info: Client 'Tom' <1> (127.0.0.1:52029) disconnected
					parts := strings.Split(info.Data, " ")
					op := parts[len(parts)-1]
					if op == "connected" {
						path := parts[len(parts)-2]
						path = path[1 : len(path)-1]
						name := strings.Join(parts[2:len(parts)-3], " ")
						name = name[1 : len(name)-1]
						//fmt.Println("Path", path, "Name", name)
						for i := 0; i < len(connections); i++ {
							addr := connections[i].LocalAddr.String()
							//fmt.Println("NPath:", addr, "Path:", path, addr == path)
							if addr == path {
                                go connections[i].delayWelcome()
                                for j:=0; j<len(connections); j++ {
                                    if i!=j {
								        connections[j].Message("", connections[i].Name + " has joined.", 0x02)
                                    }
                                }
							}
						}
					}
                } else if info.Type == "serverup" {
					fmt.Println("Server listening for connections.")
				} else if info.Type == "chat" {
					name_start := strings.Index(info.Data,"<")
                    name_end := strings.Index(info.Data,">")
                    user := info.Data[name_start+1:name_end]
					parts := strings.Split(info.Data[name_end+2:], " ")
					message := strings.Join(parts, " ")
					fmt.Println("<" + user + "> " + message)
					if string(message[0]) == "/" {
						command := parts[0][1:len(parts[0])]
						if command == "nick" {
						} else {
							for j := 0; j < len(connections); j++ {
								conn := connections[j]
								if conn.Name == user {
                                    resp := processCommand(command, parts[1:], &conn)
									for i := 0; i < len(resp); i++ {
										conn.Console(resp[i])
									}
                                    break
								}
							}
						}
					}
				} else if info.Type == "worldup" {
                    worlds = append(worlds, info.Data)
                    fmt.Println("[World] Up: "+info.Data)
                } else if info.Type == "worlddown" {
                    fmt.Println("[World] Down: "+info.Data)
                    for i:=0;i<len(worlds);i++ {
                        if worlds[i] == info.Data {
                            worlds = append(worlds[:i],worlds[i+1:]...)
                        }
                    }
                    //worlds = append(worlds, info.Data)
                } else {
					fmt.Println("[ServerMon] Info:", info.Type, "Data:", info.Data)
				}
			}
		case client, ok := <-clientChan:
			if !ok {
				fmt.Println("Server Monitor channel closed!")
			} else {
				connections = append(connections, client)
			}
		}
	}
}

func checkError(err error) {
	if err != nil {
		fmt.Println("Fatal error ", err.Error())
	}
}
