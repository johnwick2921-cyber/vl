// ConnectJsonParser.cs — pure, NT8-free parsing + reconnect policy, moved out of
// VLTraderTCPClient.cs so the K5 strict-validation logic and the K2 backoff cap are
// unit-testable without NinjaTrader assemblies. Same namespace as the AddOn.
using System;
using System.Collections.Generic;
using System.Globalization;
using System.Text;

namespace NinjaTrader.NinjaScript.AddOns
{
    // Parses connect.json (host / port / connect_key). Once the file exists, host and
    // port are REQUIRED; a present-but-non-string host/connect_key is an ERROR (no
    // silent fallback to the local defaults). Trailing garbage after the root object is
    // rejected. The key is never echoed into err or any log line.
    internal static class ConnectJsonParser
    {
        public static void TryParse(string json, out string host, out int port, out string key, out string err)
        {
            host = ""; port = 0; key = ""; err = null;

            object parsed;
            try { parsed = new JsonParser(json).ParseComplete(); }
            catch (Exception ex) { err = ex.Message; return; }

            var root = parsed as Dictionary<string, object>;
            if (root == null) { err = "connect.json is not a JSON object"; return; }

            // host — REQUIRED when the file exists.
            if (root.TryGetValue("host", out object h))
            {
                if (!(h is string sh) || string.IsNullOrEmpty(sh))
                { err = "connect.json host must be a non-empty string"; return; }
                host = sh;
            }
            else { err = "connect.json host is required"; return; }

            // port — REQUIRED when the file exists.
            if (root.TryGetValue("port", out object p))
            {
                long pl;
                if (p is long l) pl = l;
                else if (p is double d && d == Math.Truncate(d)) pl = (long)d;
                else { err = "connect.json port is not a whole number"; return; }
                if (pl <= 0 || pl > 65535) { err = "connect.json port out of range"; return; }
                port = (int)pl;
            }
            else { err = "connect.json port is required"; return; }

            // connect_key — optional; if present it MUST be a string (no silent fallback).
            if (root.TryGetValue("connect_key", out object k))
            {
                if (!(k is string sk)) { err = "connect.json connect_key must be a string"; return; }
                if (!string.IsNullOrEmpty(sk)) key = sk;
            }
        }
    }

    // Exponential reconnect backoff: 5s -> 10s -> 20s -> 40s -> 60s (cap). Pure so the
    // harness can pin the sequence; the caller adds jitter and clamps to maxMs.
    internal static class ReconnectPolicy
    {
        public static int NextBackoffMs(int current, int maxMs)
        {
            int doubled = current * 2;
            return doubled > maxMs ? maxMs : doubled;
        }
    }

    // Minimal JSON parser (moved verbatim from VLTraderTCPClient.cs, plus ParseComplete
    // which rejects trailing non-whitespace after the root value — K5).
    internal sealed class JsonParser
    {
        private readonly string s;
        private int pos;

        public JsonParser(string input)
        {
            s = input ?? string.Empty;
            pos = 0;
        }

        public object Parse()
        {
            SkipWs();
            return ParseValue();
        }

        public object ParseComplete()
        {
            SkipWs();
            object v = ParseValue();
            SkipWs();
            if (pos < s.Length) throw new Exception("trailing characters after JSON at " + pos);
            return v;
        }

        private object ParseValue()
        {
            SkipWs();
            if (pos >= s.Length) throw new Exception("unexpected EOF");
            char c = s[pos];
            if (c == '{') return ParseObject();
            if (c == '"') return ParseString();
            if (c == 't' || c == 'f') return ParseBool();
            if (c == 'n') return ParseNull();
            if (c == '-' || (c >= '0' && c <= '9')) return ParseNumber();
            if (c == '[') return ParseArray();
            throw new Exception("unexpected char '" + c + "' at " + pos);
        }

        private Dictionary<string, object> ParseObject()
        {
            var result = new Dictionary<string, object>();
            pos++; // consume '{'
            SkipWs();
            if (pos < s.Length && s[pos] == '}') { pos++; return result; }
            while (true)
            {
                SkipWs();
                string key = ParseString();
                SkipWs();
                if (pos >= s.Length || s[pos] != ':') throw new Exception("expected ':' at " + pos);
                pos++; // consume ':'
                SkipWs();
                object value = ParseValue();
                result[key] = value;
                SkipWs();
                if (pos < s.Length && s[pos] == ',') { pos++; continue; }
                if (pos < s.Length && s[pos] == '}') { pos++; return result; }
                throw new Exception("expected ',' or '}' at " + pos);
            }
        }

        private string ParseString()
        {
            if (pos >= s.Length || s[pos] != '"') throw new Exception("expected string at " + pos);
            pos++; // consume opening '"'
            var sb = new StringBuilder();
            while (pos < s.Length && s[pos] != '"')
            {
                if (s[pos] == '\\' && pos + 1 < s.Length)
                {
                    char esc = s[pos + 1];
                    switch (esc)
                    {
                        case '"':  sb.Append('"'); pos += 2; break;
                        case '\\': sb.Append('\\'); pos += 2; break;
                        case '/':  sb.Append('/'); pos += 2; break;
                        case 'n':  sb.Append('\n'); pos += 2; break;
                        case 'r':  sb.Append('\r'); pos += 2; break;
                        case 't':  sb.Append('\t'); pos += 2; break;
                        case 'b':  sb.Append('\b'); pos += 2; break;
                        case 'f':  sb.Append('\f'); pos += 2; break;
                        case 'u':
                            if (pos + 5 < s.Length)
                            {
                                sb.Append((char)Convert.ToInt32(s.Substring(pos + 2, 4), 16));
                                pos += 6;
                            }
                            else
                            {
                                pos++;
                            }
                            break;
                        default: sb.Append(esc); pos += 2; break;
                    }
                }
                else
                {
                    sb.Append(s[pos]);
                    pos++;
                }
            }
            if (pos >= s.Length) throw new Exception("unterminated string");
            pos++; // consume closing '"'
            return sb.ToString();
        }

        private object ParseNumber()
        {
            int start = pos;
            if (s[pos] == '-') pos++;
            while (pos < s.Length &&
                   (char.IsDigit(s[pos]) || s[pos] == '.' ||
                    s[pos] == 'e' || s[pos] == 'E' ||
                    s[pos] == '+' || s[pos] == '-'))
            {
                pos++;
            }
            string num = s.Substring(start, pos - start);
            if (num.IndexOfAny(new[] { '.', 'e', 'E' }) >= 0)
            {
                return double.Parse(num, CultureInfo.InvariantCulture);
            }
            if (long.TryParse(num, NumberStyles.Integer, CultureInfo.InvariantCulture, out long l))
            {
                return l;
            }
            return double.Parse(num, CultureInfo.InvariantCulture);
        }

        private bool ParseBool()
        {
            if (pos + 4 <= s.Length && s.Substring(pos, 4) == "true") { pos += 4; return true; }
            if (pos + 5 <= s.Length && s.Substring(pos, 5) == "false") { pos += 5; return false; }
            throw new Exception("invalid bool at " + pos);
        }

        private object ParseNull()
        {
            if (pos + 4 <= s.Length && s.Substring(pos, 4) == "null") { pos += 4; return null; }
            throw new Exception("invalid null at " + pos);
        }

        private List<object> ParseArray()
        {
            var result = new List<object>();
            pos++; // consume '['
            SkipWs();
            if (pos < s.Length && s[pos] == ']') { pos++; return result; }
            while (true)
            {
                SkipWs();
                result.Add(ParseValue());
                SkipWs();
                if (pos < s.Length && s[pos] == ',') { pos++; continue; }
                if (pos < s.Length && s[pos] == ']') { pos++; return result; }
                throw new Exception("expected ',' or ']' at " + pos);
            }
        }

        private void SkipWs()
        {
            while (pos < s.Length &&
                   (s[pos] == ' ' || s[pos] == '\t' ||
                    s[pos] == '\n' || s[pos] == '\r'))
            {
                pos++;
            }
        }
    }
}
