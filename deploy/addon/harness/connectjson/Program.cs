using System;
using System.IO;
using NinjaTrader.NinjaScript.AddOns;

static class Program
{
    static int _pass = 0, _fail = 0;

    static int Main()
    {
        // ---- K5: ConnectJsonParser.TryParse (strict connect.json) ----
        ParseOk("valid", "{\"host\":\"127.0.0.2\",\"port\":18081,\"connect_key\":\"abc\"}",
                "127.0.0.2", 18081, "abc", null);
        ParseErr("missing host", "{\"port\":18081}", "connect.json host is required");
        ParseErr("missing port", "{\"host\":\"127.0.0.1\"}", "connect.json port is required");
        ParseErr("host non-string", "{\"host\":123,\"port\":18081}", "connect.json host must be a string");
        ParseErr("empty host", "{\"host\":\"\",\"port\":18081}", "connect.json host must be a non-empty string");
        ParseErr("key non-string", "{\"host\":\"h\",\"port\":1,\"connect_key\":123}", "connect.json connect_key must be a non-empty string");
        ParseErr("empty key (C15)", "{\"host\":\"h\",\"port\":1,\"connect_key\":\"\"}", "connect.json connect_key must be a non-empty string");
        ParseErr("trailing garbage", "{\"host\":\"h\",\"port\":1} x", "trailing");
        ParseErr("port non-whole", "{\"host\":\"h\",\"port\":1.5}", "connect.json port is not a whole number");
        ParseErr("port out of range", "{\"host\":\"h\",\"port\":70000}", "connect.json port out of range");
        ParseErr("duplicate key", "{\"host\":\"a\",\"host\":\"b\",\"port\":1}", "duplicate");
        ParseOk("trim host (C15)", "{\"host\":\"  127.0.0.2  \",\"port\":1}", "127.0.0.2", 1, "", null);

        // ---- K2: ReconnectPolicy (5s -> 60s cap + jitter clamp) ----
        Eq("backoff 5s->10s", 10000, ReconnectPolicy.NextBackoffMs(5000, 60000));
        Eq("backoff 10s->20s", 20000, ReconnectPolicy.NextBackoffMs(10000, 60000));
        Eq("backoff 20s->40s", 40000, ReconnectPolicy.NextBackoffMs(20000, 60000));
        Eq("backoff 40s capped to 60s", 60000, ReconnectPolicy.NextBackoffMs(40000, 60000));
        Eq("backoff 30s capped to 60s", 60000, ReconnectPolicy.NextBackoffMs(30000, 60000));
        Eq("backoff stays at cap", 60000, ReconnectPolicy.NextBackoffMs(60000, 60000));
        Eq("jitter=0 next", 10000, ReconnectPolicy.NextJitteredBackoffMs(5000, 5000, 60000, 0));
        Eq("jitter clamps to max", 60000, ReconnectPolicy.NextJitteredBackoffMs(40000, 5000, 60000, 500));
        Eq("jitter clamps to min", 5000, ReconnectPolicy.NextJitteredBackoffMs(3000, 5000, 60000, -4000));

        // ---- C14: ConnectJsonParser.ReadConnectJson (absent vs unreadable) ----
        string tmpDir = Path.Combine(Path.GetTempPath(), "vl-connectjson-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(tmpDir);
        string tmp = Path.Combine(tmpDir, "connect.json");
        File.WriteAllText(tmp, "{\"host\":\"h\",\"port\":1}");
        ReadOk("read ok", tmp);
        ReadAbsent("read absent", Path.Combine(tmpDir, "missing.json"));
        ReadUnreadable("read unreadable (dir)", tmpDir);

        Console.WriteLine($"pins: {_pass} PASS, {_fail} FAIL");
        return _fail == 0 ? 0 : 1;
    }

    static void ParseOk(string name, string json, string wantHost, int wantPort, string wantKey, string wantErr)
    {
        string host; int port; string key; string err;
        ConnectJsonParser.TryParse(json, out host, out port, out key, out err);
        bool ok = host == wantHost && port == wantPort && key == wantKey && err == wantErr;
        Report(name, ok, ok ? "" : $"got host={host} port={port} key={key} err={err}");
    }

    static void ParseErr(string name, string json, string wantErrSubstring)
    {
        string host; int port; string key; string err;
        ConnectJsonParser.TryParse(json, out host, out port, out key, out err);
        bool ok = err != null && err.Contains(wantErrSubstring);
        Report(name, ok, ok ? "" : $"got err={err}");
    }

    static void ReadOk(string name, string path)
    {
        string json; string err;
        var rr = ConnectJsonParser.ReadConnectJson(path, out json, out err);
        Report(name, rr == ConnectJsonRead.Ok && json.Length > 0 && err == null,
               rr == ConnectJsonRead.Ok ? "" : $"rr={rr} err={err}");
    }

    static void ReadAbsent(string name, string path)
    {
        string json; string err;
        var rr = ConnectJsonParser.ReadConnectJson(path, out json, out err);
        Report(name, rr == ConnectJsonRead.Absent, $"rr={rr} err={err}");
    }

    static void ReadUnreadable(string name, string path)
    {
        string json; string err;
        var rr = ConnectJsonParser.ReadConnectJson(path, out json, out err);
        Report(name, rr == ConnectJsonRead.Unreadable, $"rr={rr} err={err}");
    }

    static void Eq(string name, int want, int got)
    {
        Report(name, want == got, want == got ? "" : $"got {got}, want {want}");
    }

    static void Report(string name, bool ok, string failDetail)
    {
        if (ok) { _pass++; Console.WriteLine($"PASS {name}"); }
        else { _fail++; Console.WriteLine($"FAIL {name}: {failDetail}"); }
    }
}
