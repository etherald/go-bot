package main

import (
    "exec";
    "fmt";
    "go/ast";
    "go/parser";
    "io";
    "os";
    "rand";
    "regexp";
    "strings";
    "syscall";
    "time";
)

type EvalResult struct {
    response []byte; //partial output from program
    compiler []byte; //compiler error
    incomplete, killed, started bool;
}
type RunConfig struct {
    run, single bool;
    goroot string;
    runpath string;
    main string;
}

const resultBufferSize = 128; //for both errors and output

func GetDefaultConfig() *RunConfig {
    config := new(RunConfig);
    config.runpath = "/tmp";
    //the modified go root I'm using
    config.goroot = os.Getenv("GOROOT") + "/safe";
    return config;
}
func (config *RunConfig) AssignExpr(expr string) os.Error {
    config.single = true;
    return config.AssignMain("fmt.Print(" + expr + ")");
}
func (config *RunConfig) AssignMain(main string) os.Error {
    //allowed list in $GOROOT/go-bot/modules has allowed list: this is where I personally store it
    modfile, err := io.ReadFile(os.Getenv("GOROOT") + "/go-bot/modules");
    items := strings.Split(string(modfile), "\n", 0);
    modules := "";
    for _, item := range items {
        if len(item) == 0 { continue }
        nameditem := strings.Split(item, "/", 0); //get part after '/', if exists
        if (strings.Index(main, nameditem[len(nameditem)-1] + ".") != -1) {
            modules += fmt.Sprintf(`"%s"; `, item);
        }
    }
    config.main =
        "package main\n"
        "import (" + modules + ")\n"
        "func main() {\n"
        + main + ";\n"
        "}\n";
    return config.verify();
}
func (config *RunConfig) verify() os.Error {
    //parse tree information will be used more in-depth in the future
    file, err := parser.ParseFile("", config.main, 0);
    if (err != nil) {
        //compile, but don't run: compiler gives more useful error messages
        return nil
    }
    var body *ast.BlockStmt;
    for _, undecl := range file.Decls {
        switch decl := undecl.(type) {
        case *ast.GenDecl:
            //d.Specs <- contains import statements, should check this if we allow free imports
        case *ast.FuncDecl:
            if decl.Name.Value == "main" {
                body = decl.Body
            } else {
                return os.NewError("Statements not contained");
            }
        }
    }
    if body == nil {
        return os.NewError("Internal error: could not find main")
    }
    //TODO: deduction of whether main is entirely an expression-statement or not
    if config.single && len(body.List) > 1 { //should only be one statement
        return os.NewError("Expression not contained");
    }
    config.run = true;
    return nil;
}
func randalphanum(leng int) string {
    arr := make([]byte, leng);
    for i := 0; i < leng; i++ {
        arr[i] = uint8(rand.Int()%26) + 'a';
    }
    return string(arr);
}

func (result *EvalResult) Format(err os.Error) string {
    if !result.started {
        if result.compiler != nil {
            unlines := strings.Split(string(result.compiler), "\n", 0);
            errors := "";
            for i, s := range unlines {
                space := strings.Index(s, " "); //tmp/blah.go:8: error
                if space == -1 { continue }
                s = s[space+1:len(s)];
                if len(errors) + len(s) > resultBufferSize { errors += "..."; break }
                //all this concatenating is not healthy
                if len(errors) > 0 { errors += ", " }
                errors += s;
            }
            err = os.NewError(errors);
        }
        return " <Error: " + err.String() + ">";
    }
    var response string;
    if len(result.response) == 0 {
        response = "<no output>"
    } else {
        response = sansControlCharacters(result.response);
    }
    if result.incomplete { response += "..." }
    if result.killed { response = "<killed> " + response}
    return response;
}
func sansControlCharacters(x []byte) string {
    for index, elem := range x {
        if (elem < 32) {
            x[index] = ' ';
        }
    }
    return string(x);
}

func RunGoProgram(config *RunConfig) (result *EvalResult, err os.Error) {
    fname := randalphanum(8) + "gen";
    fpathname := config.runpath + "/" + fname;
    fgo := fpathname + ".go";

    fmt.Printf("----- %s -----\n", fgo);
    fmt.Println(config.main);
    //output file
    io.WriteFile(fgo, strings.Bytes(config.main), 0666);
    defer os.Remove(fgo);

    result = new(EvalResult); //this is the return value!

    //get environment, modified to be the "$GOROOT/safe"
    binpath := config.goroot + "/bin";
    envPrime := make([]string, len(os.Envs));
	for i, s := range os.Envs {
        if strings.HasPrefix(s, "GOROOT=") {
            envPrime[i] = "GOROOT=" + config.goroot;
        } else {
            envPrime[i] = s;
        }
	}

    //compile
    fmt.Printf("Compiling %s %s.\n", "8g", fgo);
    cmd, err := exec.Run(binpath + "/8g", []string{"8g", "-o", fpathname + ".8", fgo}, envPrime, exec.Pipe, exec.Pipe, exec.MergeWithStdout);
    if err != nil { return }
    bstr, err := io.ReadAll(cmd.Stdout);
    if err != nil { return }
    wait, err := cmd.Wait(0);
    if err != nil || wait.WaitStatus != 0 || !config.run { result.compiler = bstr; return }
    defer os.Remove(fpathname + ".8");

    //link
    fmt.Printf("Linking %s %s.8.\n", "8l", fpathname);
    cmd, err = exec.Run(binpath + "/8l", []string{"8l", "-o", fpathname, fpathname + ".8"}, envPrime, exec.PassThrough, exec.PassThrough, exec.MergeWithStdout);
    if err != nil { return }
    wait, err = cmd.Wait(0);
    cmd.Close();
    if err != nil || wait.WaitStatus != 0 { err = os.NewError("Failed to link"); return }
    defer os.Remove(fpathname);

    //load
    fmt.Printf("Running %s.\n", fpathname);
    result.started = true;
    cmd, err = exec.Run(fpathname, []string{fname}, envPrime, exec.Pipe, exec.Pipe, exec.MergeWithStdout);
    if err != nil { return nil, err }
    murder := make(chan bool, 1);
    go func() { //don't spend too much time
        time.Sleep(3 * 1000000000);
        if murder <- true {
            syscall.Kill(cmd.Pid, syscall.SIGKILL);
        }
    }();
    //collect as much data under resultBufferSize as we can
    response := make([]byte, resultBufferSize+1);
    temponse := response;
    total, num := 0, 0;
    for total <= resultBufferSize && err == nil {
        temponse = temponse[num:len(temponse)];
        num, err = cmd.Stdout.Read(temponse);
        total += num;
    }
    if total == resultBufferSize+1 {
        total--;
        result.incomplete = true;
    }
    //kill, if not killed already
    if murder <- true {
        //should probably also check if died of natural causes
        syscall.Kill(cmd.Pid, syscall.SIGKILL);
    } else {
        result.killed = true;
    }
    result.response = response[0:total];
    return;
}
