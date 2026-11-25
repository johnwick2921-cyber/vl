// Stub NT8 types so the REAL ninjascript/VLTraderTCPClient.cs compiles and runs
// outside NinjaTrader. Only what that file touches. Account models what the NT8
// trace files showed on SIM: one fill thread for every account, callbacks raised
// synchronously on the caller's thread (Submit / Cancel), and Flatten sizing its
// Close order at the call but building it only after the cancels (113-237 ms).
using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Linq;
using System.Threading;

namespace NinjaTrader.Core { public static class Globals { public static readonly DateTime MaxDate = DateTime.MaxValue; public static string UserDataDir = "/nonexistent"; } }

namespace NinjaTrader.Data { }

namespace NinjaTrader.Cbi
{
    public enum OrderState { Accepted, Cancelled, CancelPending, CancelSubmitted, ChangePending, ChangeSubmitted, Filled, Initialized, PartFilled, Rejected, Submitted, TriggerPending, Unknown, Working }
    public enum OrderAction { Buy, BuyToCover, Sell, SellShort }
    public enum OrderType { Limit, Market, MIT, StopMarket, StopLimit }
    public enum OrderEntry { Automated, Manual }
    public enum TimeInForce { Day, Gtc }
    public enum MarketPosition { Long, Short, Flat }
    public enum ConnectionStatus { Connected, Connecting, ConnectionLost, Disconnected, Disconnecting }
    public enum AccountItem { CashValue, BuyingPower, RealizedProfitLoss, UnrealizedProfitLoss }
    public enum Currency { UsDollar }
    public enum ErrorCode { NoError, OrderRejected }

    public class MasterInstrument { public string Name = ""; public double TickSize = 0.25; }
    public class Instrument
    {
        public string Full = "MNQ 12-26";
        public static int ThrowFullNameIn = -1; // fault injection: throws on the Nth read
        public string FullName { get { if (ThrowFullNameIn >= 0 && ThrowFullNameIn-- == 0) throw new InvalidOperationException("stub FullName"); return Full; } }
        public MasterInstrument MasterInstrument = new MasterInstrument { Name = "MNQ" };
        public static readonly Instrument Mnq = new Instrument();
        public static readonly Instrument Es = new Instrument { Full = "MES 12-26", MasterInstrument = new MasterInstrument { Name = "MES" } };
    }
    public class Connection
    {
        public static readonly List<Connection> Connections = new List<Connection>();
        public static event EventHandler<ConnectionStatusEventArgs> ConnectionStatusUpdate;
        public ConnectionStatus Status = ConnectionStatus.Connected, PriceStatus = ConnectionStatus.Connected;
        public static void Raise(ConnectionStatusEventArgs e) { ConnectionStatusUpdate?.Invoke(null, e); }
    }
    public class ConnectionStatusEventArgs : EventArgs { public Connection Connection; public ConnectionStatus Status, PreviousStatus, PriceStatus, PreviousPriceStatus; }
    public class AccountStatusEventArgs : EventArgs { public Account Account; public ConnectionStatus Status; }
    public class AccountItemEventArgs : EventArgs { public AccountItem AccountItem; }
    public class Position { public Instrument Instrument; public MarketPosition MarketPosition = MarketPosition.Flat; public int Quantity; public double AveragePrice; }
    public class PositionEventArgs : EventArgs { public Position Position; public MarketPosition MarketPosition; public int Quantity; public double AveragePrice; }
    public class OrderEventArgs : EventArgs { public Order Order; public OrderState OrderState; public int Filled; public double AverageFillPrice; public string Comment = ""; public ErrorCode Error = ErrorCode.NoError; }

    public class Order
    {
        public string Name = "", Oco = "", OrderId = "";
        public Account Account; public Instrument Instrument;
        public OrderAction OrderAction; public OrderType OrderType; public TimeInForce TimeInForce;
        public int Quantity; public double LimitPrice, StopPrice, AverageFillPrice;
        public double StopPriceChanged, LimitPriceChanged; public int QuantityChanged;
        public volatile OrderState OrderStateV = OrderState.Initialized;
        public OrderState OrderState { get { return OrderStateV; } set { OrderStateV = value; } }
        public static int ThrowFilledIn = -1;
        private int filled;
        public int Filled { get { if (ThrowFilledIn >= 0 && ThrowFilledIn-- == 0) throw new InvalidOperationException("stub Filled"); return filled; } set { filled = value; } }
        public int FilledRaw { get { return filled; } }
        public bool Terminal { get { return OrderState == OrderState.Filled || OrderState == OrderState.Cancelled || OrderState == OrderState.Rejected; } }
        public bool Buyish { get { return OrderAction == OrderAction.Buy || OrderAction == OrderAction.BuyToCover; } }
    }

    public class Account
    {
        public static readonly List<Account> All = new List<Account>();
        public string Name; public bool Simulation { get; set; } = true;
        public Connection Connection = new Connection();
        private readonly List<Position> positions = new List<Position>();
        private readonly List<Order> orders = new List<Order>();
        public int ThrowPositionsIn = -1;
        public List<Position> Positions { get { if (ThrowPositionsIn >= 0 && ThrowPositionsIn-- == 0) throw new InvalidOperationException("stub Positions"); return positions; } }
        public List<Order> Orders { get { return orders; } }
        public event EventHandler<OrderEventArgs> OrderUpdate;
        public event EventHandler<PositionEventArgs> PositionUpdate;
        public event EventHandler<AccountItemEventArgs> AccountItemUpdate;

        // What the AddOn asked NT8 to do, in order, with the position at that moment.
        public readonly ConcurrentQueue<string> Calls = new ConcurrentQueue<string>();
        public Action<string> OnCall;         // test hook, runs inside the call on the caller's thread
        public bool ThrowOnSubmit, ThrowOnFlatten;
        public int CloseDelayMs = 130;        // Flatten call -> its Close order (trace: 113-237)
        public bool CloseRejects;             // the Close order is rejected instead of filled
        public object ApiLock;                // non-null: held while Cancel raises (deadlock harness)
        public int Pos;                       // signed MNQ position

        public static readonly BlockingCollection<Action> Sim = new BlockingCollection<Action>();
        static Account()
        {
            var t = new Thread(() => { foreach (var a in Sim.GetConsumingEnumerable()) { try { a(); } catch (Exception ex) { Console.WriteLine("SIM THREAD: " + ex); } } }) { IsBackground = true, Name = "stub-sim" };
            t.Start();
        }
        public static void OnSim(Action a) { Sim.Add(a); }
        public static void SimWait(Action a) { var d = new ManualResetEventSlim(); Sim.Add(() => { try { a(); } finally { d.Set(); } }); if (!d.Wait(10000)) throw new TimeoutException("sim thread stuck"); }

        private void Call(string s) { Calls.Enqueue(s); var h = OnCall; if (h != null) h(s); }
        public int Count(string prefix) { return Calls.Count(c => c.StartsWith(prefix)); }
        public double Get(AccountItem i, Currency c) { return 0; }

        public Order CreateOrder(Instrument i, OrderAction a, OrderType t, OrderEntry e, TimeInForce tif, int qty, double limit, double stop, string oco, string name, DateTime gtd, object custom)
        {
            return new Order { Account = this, Instrument = i, OrderAction = a, OrderType = t, TimeInForce = tif, Quantity = qty, LimitPrice = limit, StopPrice = stop, Oco = oco, Name = name, OrderId = Guid.NewGuid().ToString("N") };
        }
        public void Submit(IEnumerable<Order> os)
        {
            if (ThrowOnSubmit) throw new InvalidOperationException("stub submit");
            foreach (var o in os)
            {
                Call("Submit " + o.Name + " " + o.OrderAction + " " + o.OrderType + " " + o.Quantity);
                lock (orders) orders.Add(o);
                State(o, OrderState.Submitted);
            }
        }
        public void Cancel(IEnumerable<Order> os)
        {
            foreach (var o in os.ToList())
            {
                Call("Cancel " + o.Name);
                if (o.Terminal) continue;
                if (ApiLock != null) { lock (ApiLock) { State(o, OrderState.CancelPending); } }
                else State(o, OrderState.CancelPending);
                OnSim(() => { if (!o.Terminal) State(o, OrderState.Cancelled); });
            }
        }
        public void Change(IEnumerable<Order> os) { foreach (var o in os) Call("Change " + o.Name); }
        public void Flatten(ICollection<Instrument> ins)
        {
            int size = Pos; // sized at the call; whether NT8 re-reads it is not known (0 cases)
            Call("Flatten pos=" + size);
            if (ThrowOnFlatten) throw new InvalidOperationException("stub flatten");
            List<Order> live; lock (orders) live = orders.Where(o => !o.Terminal).ToList();
            foreach (var o in live) { if (o.Terminal) continue; State(o, OrderState.CancelPending); }
            OnSim(() => { foreach (var o in live) if (!o.Terminal) State(o, OrderState.Cancelled); });
            if (size == 0) return;
            new Thread(() =>
            {
                Thread.Sleep(CloseDelayMs);
                OnSim(() =>
                {
                    Call("Close2 " + size);
                    var c = CreateOrder(Instrument.Mnq, size > 0 ? OrderAction.Sell : OrderAction.BuyToCover, OrderType.Market, OrderEntry.Manual, TimeInForce.Gtc, Math.Abs(size), 0, 0, "", "Close", DateTime.MaxValue, null);
                    lock (orders) orders.Add(c);
                    State(c, OrderState.Submitted);
                    if (CloseRejects) { State(c, OrderState.Rejected); return; }
                    FillNow(c, c.Quantity);
                });
            }) { IsBackground = true }.Start();
        }

        // ── events, on the calling thread ──
        public void State(Order o, OrderState s)
        {
            o.OrderState = s;
            OrderUpdate?.Invoke(this, new OrderEventArgs { Order = o, OrderState = s, Filled = o.FilledRaw, AverageFillPrice = 100 });
        }
        public void FillEvent(Order o, int cum)   // the order half: live state, then the callback
        {
            o.Filled = cum; o.OrderState = cum >= o.Quantity ? OrderState.Filled : OrderState.PartFilled;
            OrderUpdate?.Invoke(this, new OrderEventArgs { Order = o, OrderState = o.OrderState, Filled = cum, AverageFillPrice = 100 });
        }
        public void MovePos(int delta)            // the position half
        {
            SetPos(Pos + delta); RaisePos();
        }
        public void SetPos(int p)                 // silent (NT8 restart restore, 10-22)
        {
            Pos = p;
            lock (positions)
            {
                positions.RemoveAll(x => x.Instrument == Instrument.Mnq);
                if (p != 0) positions.Add(new Position { Instrument = Instrument.Mnq, MarketPosition = p > 0 ? MarketPosition.Long : MarketPosition.Short, Quantity = Math.Abs(p), AveragePrice = 100 });
            }
        }
        public void RaisePos()
        {
            var mp = Pos > 0 ? MarketPosition.Long : Pos < 0 ? MarketPosition.Short : MarketPosition.Flat;
            PositionUpdate?.Invoke(this, new PositionEventArgs { Position = new Position { Instrument = Instrument.Mnq, MarketPosition = mp, Quantity = Math.Abs(Pos) }, MarketPosition = mp, Quantity = Math.Abs(Pos), AveragePrice = 100 });
        }
        public void FillNow(Order o, int cum) { int d = cum - o.FilledRaw; FillEvent(o, cum); if (d > 0) MovePos(o.Buyish ? d : -d); }
        public Order Find(string nameEnd) { lock (orders) return orders.LastOrDefault(o => o.Name.EndsWith(nameEnd)); }
    }
}

namespace NinjaTrader.NinjaScript
{
    public enum State { SetDefaults, Active, Terminated }
    public class AddOnBase { public State State; public string Name, Description; protected virtual void OnStateChange() { } }
    public static class NinjaScript
    {
        public static readonly ConcurrentQueue<string> Lines = new ConcurrentQueue<string>();
        public static Action<string> OnLog; // test hook, on the caller's thread
        public static void Log(string m, NinjaTrader.Cbi.LogLevel l) { Lines.Enqueue(l + " " + m); var h = OnLog; if (h != null) h(m); }
    }
}
namespace NinjaTrader.Cbi { public enum LogLevel { Information, Warning, Error } }

namespace NinjaTrader.NinjaScript.AddOns
{
    using NinjaTrader.Cbi;
    public static class VLContractResolver { public static string ResolveFrontMonthContract(string s) { return s + " 12-26"; } }
    public static class VLInstrumentLookup
    {
        public static int ThrowIn = -1;
        public static Instrument Resolve(string symbol, Action<string> w, out string how)
        {
            how = "stub";
            if (ThrowIn >= 0 && ThrowIn-- == 0) throw new InvalidOperationException("stub resolve");
            return symbol == "MNQ" ? Instrument.Mnq : symbol == "MES" ? Instrument.Es : null;
        }
        public static string ContractName(Instrument i) { return i.Full; }
    }
    public class VLBarsSubscriptionManager
    {
        public VLBarsSubscriptionManager(Action<string, Dictionary<string, object>> s, Action<string> i, Action<string> w) { }
        public void HandleBarsSubscribe(Dictionary<string, object> p) { }
        public void HandleBarsUnsubscribe(Dictionary<string, object> p) { }
        public void OnConnectionReconnected() { }
        public void GetFeedFreshness(out long a, out long b, out string c, out string d, out string e) { a = b = 0; c = d = e = ""; }
        public void DisposeAll() { }
    }
    public class VLHistoryPullManager
    {
        public VLHistoryPullManager(Action<string, Dictionary<string, object>> s, Action<string> i, Action<string> w) { }
        public void HandleBarsHistoryRequest(Dictionary<string, object> p) { }
        public void DisposeAll() { }
    }
}
