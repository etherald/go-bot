package main

import (
    "bufio";
    "fmt";
    "io";
    "os";
    "rand";
    "regexp";
    "strings";
    "syscall";
    "time";
)

//to implement: imported definitons (to define methods for interfaces, to use). send results in a private message
//database from functionality -> module. also, database defining interface instances.

var nick string = ""; //insert bot name here

func quickTest() {
    result := new(EvalResult);
    config := GetDefaultConfig();
    err := config.AssignExpr(`"Hello world"`);
    if err == nil {
        result, err = RunGoProgram(config);
    }
    fmt.Println(result.Format(err));
}
func main() {
    rand.Seed(time.Nanoseconds());
    //hack: we allocate less than 64MB, so set our rlimit so subprocesses have the same.
    syscall.Setrlimit(9, &syscall.Rlimit{1048576*64, 1048576*64}); //RLIMIT_AS, 64 MB
    quickTest();

    con := NewConnection(); //Connect later

    runreg := regexp.MustCompile("^@(main|eval) |^> ");
    evals := con.NewChan(func(evt *IRCEvent) bool {
        return evt.form == CHAN_MESSAGE && runreg.MatchString(evt.message);
    });
    results := make(chan string, 20);
    accept := false; //another control hack

    evaluator := func() {
        for {
            evt := <-evals;
            if (!accept) { continue }
            msg := evt.message;
            parts := runreg.ExecuteString(msg);
            input := msg[parts[1]:len(msg)];
            var str string;
            result := new(EvalResult);
            var err os.Error;
            if parts[2] == -1 {
                str = ">";
            } else {
                str = msg[parts[2]:parts[3]];
            }
            config := GetDefaultConfig();
            switch str {
                case "main", ">":
                    err = config.AssignMain(input);
                case "eval":
                    err = config.AssignExpr(input);
                default: continue //nothing we can recognize
            }
            if (err == nil) {
                result, err = RunGoProgram(config);
            }
            results <- "PRIVMSG " + evt.target + " : " + result.Format(err);
        }
    };
    for i := 1; i <= 3; i++ {
        go evaluator();
    }
    //hack for control
    con.AddListener(func(evt *IRCEvent) {
        msg := evt.message;
        if msg[0] == ':' {
            con.Write <- msg[1:len(msg)]
        } else if msg == "go" {
        }
    }, func(evt *IRCEvent) bool {
        return evt.form == PRIV_MESSAGE && sufficientPermissions(evt)
    });

    stopreg := regexp.MustCompile("^@(go|stop)");
    con.AddListener(func(evt *IRCEvent) {
       msg := evt.message;
        parts := stopreg.ExecuteString(msg);
        switch msg[parts[2]:parts[3]] {
            case "go":
                accept = true;
                fmt.Println("Now accepting!");
            case "stop":
                accept = false;
                fmt.Println("Now rejecting!");
        }
    }, func(evt *IRCEvent) bool {
        return sufficientPermissions(evt) && stopreg.MatchString(evt.message);
    });

    go func() {
        for {
            str := <-results;
            if closed(results) { break }
            if (!accept) { continue }
            con.Write <- str;
            //some primitive anti-flooding sleeping
            time.Sleep(1000000000/2);
        }
    }();
    go func() {
        read := con.NewChan(func(evt *IRCEvent) bool {
            return false; //evt.form == ...;
        });
        for {
            evt := <-read;
            if closed(read) { break }
            fmt.Println(evt.raw);
        }
    }();

    err := con.Connect("irc.freenode.net:6667", nick);
    if (err != nil) { fmt.Println(err); os.Exit(0); }
    fmt.Print("Nickserv password: ");
    password, err := bufio.NewReader(os.Stdin).ReadString('\n');
    password = password[0:len(password)-1];
    if len(password) > 0 {
        con.Identify(password);
    }
    //con.Write <- "JOIN #channel";

    <-make(chan int)
}
func sufficientPermissions(evt *IRCEvent) bool { //possibly expand to a password system
    return strings.HasSuffix(evt.fullsource, "person@wikipedia/Gracenotes")
}

