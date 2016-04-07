package network

import (
    "fmt"
    "net"
    //"strconv"
    "encoding/json"
    "time"
    "netgrok/buffer"
    . "netgrok/obj"
)

func listening_worker(pop_channel chan<- Message, broadcast_port string) (<-chan Message, <-chan *net.UDPAddr, *net.UDPAddr) {
    local_addr, err := net.ResolveUDPAddr("udp4", ":" + broadcast_port);
    tail_socket, err := net.ListenUDP("udp4", local_addr);
    if err != nil {
        fmt.Println("Could not create tail socket.");
        return nil, nil, nil;
    }

    addr := &net.UDPAddr{};
    from_network_channel := make(chan Message);
    go func() {
        b := make([]byte, 1024);
        for {
            var n int;
            var err error;
            n, addr, err = tail_socket.ReadFromUDP(b);
            if err != nil {
                //fmt.Println("Could not read from UDP!");
            } else {
                if addr.String() != local_addr.String() && n > 0 {
                    msg := Message{};
                    err := json.Unmarshal(b[:n], &msg);
                    if err != nil {
                        fmt.Println("Could not unmarshal message.");
                    } else {
                        from_network_channel <-msg;

                    }
                    //fmt.Println("Received message with code", msg.Code, "with body", msg.Body, "from", addr.String());

                }
            }
        }
    }();
    rcv_channel := make(chan Message);
    tail_timeout_channel := make(chan *net.UDPAddr);
    go func() {
        for {
            select {
            case msg := <-from_network_channel:
                if msg.Code != KEEP_ALIVE && msg.Signatures[0] == tail_socket.LocalAddr().String() {
                    pop_channel <-msg;
                } else {
                    rcv_channel <-msg;
                }
            case <-time.After(4 * time.Second):
                tail_timeout_channel <-addr;
            }
        }
    }();
    return rcv_channel, tail_timeout_channel, local_addr;
}

func send(msg Message, socket *net.UDPConn, addr *net.UDPAddr) (error) {
    msg.Signatures = append(msg.Signatures, socket.LocalAddr().String());
    b, err := json.Marshal(msg);
    if err != nil {
        fmt.Println("Could not marshal message.");
    }
    _, err = socket.WriteToUDP(b, addr);
    if err != nil {
        fmt.Println("Could not send!");
    } else {
        //fmt.Println("Sent message with code", msg.Code, "and body", msg.Body, "to:", addr.String());
    }
    return err;
}

func broadcast(msg Message, socket *net.UDPConn) (error) {
    b, err := json.Marshal(msg);
    if err != nil {
        fmt.Println("Could not marshal messaglocal");
    }
    _, err = socket.Write(b);
    if err != nil {
        fmt.Println("Could not send!");
    } else {
        //fmt.Println("Sent message with code", msg.Code, "and body", msg.Body, "to:", addr.String());
    }
    return err;
}

func Manager(broadcast_port string) (chan<- Message, <-chan Message) {
    broadcast_addr, _ := net.ResolveUDPAddr("udp4", "255.255.255.255" + ":" + broadcast_port);

    var head_addr *net.UDPAddr;
    head_socket, err := net.DialUDP("udp4", nil, broadcast_addr);
    broadcast(*NewMessage(KEEP_ALIVE, []byte{}), head_socket);
    defer head_socket.Close();

    if err != nil {
        fmt.Println("Could not create socket.");
        return nil, nil;
    } else {
        fmt.Println("Sockets have been created.");
    }
    push_channel, pop_channel := buffer.Manager();
    rcv_channel, tail_timeout_channel, local_addr := listening_worker(pop_channel, broadcast_port);

    to_network_channel := make(chan Message);
    from_network_channel := make(chan Message);
    go func() {
        for {
            if head_addr == nil {
                b, _ := json.Marshal(local_addr);
                msg := *NewMessage(HEAD_REQUEST, b);
                broadcast(msg, head_socket);
                select {
                case <-time.After(10 * time.Second):
                    continue;
                case msg := <-rcv_channel:
                    switch msg.Code {
                    case TAIL_REQUEST:
                        var addr *net.UDPAddr;
                        err := json.Unmarshal(msg.Body, &addr);
                        if err != nil {
                            fmt.Println("Could not unmarshal address.");
                        } else {
                            head_addr = addr;
                            conn := NewConnection(local_addr, addr);
                            b, _ := json.Marshal(conn);
                            msg := *NewMessage(CONNECTION, b);
                            push_channel <-msg;
                            send(msg, head_socket, addr);
                        }
                    case HEAD_REQUEST:
                        //SPAWN CONNECTION
                        var addr *net.UDPAddr;
                        err := json.Unmarshal(msg.Body, &addr);
                        if err != nil {
                            fmt.Println("Could not unmarshal message.")
                        } else {
                            b, _ := json.Marshal(local_addr);
                            msg := *NewMessage(TAIL_REQUEST, b);
                            send(msg, head_socket, addr);
                        }
                    }
                }
            } else {
                select {

                case msg := <-to_network_channel:
                    push_channel <-msg;
                    send(msg, head_socket, head_addr);
                case msg :=  <-rcv_channel:
                    tail_timeout_channel = nil;
                    switch msg.Code {
                    case KEEP_ALIVE:
                        break;
                    case CONNECTION:
                        var conn Connection;
                        err := json.Unmarshal(msg.Body, &conn);
                        if err != nil {
                            fmt.Println("Could not unmarshal connection.");
                        } else {
                            if head_addr == nil || head_addr.String() == conn.To.String() {
                               head_addr = conn.From;
                            }
                            send(msg, head_socket, head_addr);
                        }
                    case HEAD_REQUEST:
                        addr := &net.UDPAddr{};
                        err := json.Unmarshal(msg.Body, &addr);
                        if err != nil {
                            fmt.Println("Could not unmarshal message.")
                        } else {
                            b, _ := json.Marshal(local_addr);
                            msg := *NewMessage(TAIL_REQUEST, b);
                            send(msg, head_socket, addr);
                        }
                    case TAIL_REQUEST:
                        break;
                    case TAIL_DEAD:
                        time.Sleep(1 * time.Second);
                        fmt.Println("Cycle broken.");
                        send(msg, head_socket, head_addr);
                        head_addr = nil;
                    default:
                        from_network_channel <-msg;
                        send(msg, head_socket, head_addr);
                    }
                case <- time.After(1 * time.Second):
                    msg := *NewMessage(KEEP_ALIVE, []byte{});
                    send(msg, head_socket, head_addr);
                case <-tail_timeout_channel:
                    fmt.Println("Breaking cycle.");
                    msg := *NewMessage(TAIL_DEAD, []byte{});
                    send(msg, head_socket, head_addr);
                    head_addr = nil;
                    tail_timeout_channel = nil;
                }
            }
        }
    }();
    return to_network_channel, from_network_channel;
}
