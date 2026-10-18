# VL friend guide — one page

You trade VL on **your own NinjaTrader** with **your own SIM account** and **your
own AI key**. Your bot runs on your friend's computer — you never see the code,
and you can only reach your own bot. This takes ~5 minutes.

## 1. Before you start

- NinjaTrader 8 installed, **connected** to your live data feed, and your **SIM
  account** selected.
- The **install code** your friend sent you (one line of text).

## 2. Install

1. Run **`VL-Setup.exe`**.
   - Windows **SmartScreen** may show "Windows protected your PC" because the app
     is not yet code-signed. Click **More info → Run anyway**. That is normal for
     now; a signed version comes later.
2. Paste your **install code** and click **Install**.
   - This joins a private tunnel (Tailscale), writes your connection file, and
     places the compiled VL add-on into NinjaTrader.
3. **Restart NinjaTrader** (the add-on loads on start). If prompted, confirm the
   add-on is allowed.
4. Double-click the **VL** desktop icon.

Your dashboard opens like an app, showing **your** bot.

## 3. First login

1. On the login screen, create your account (first run makes it).
2. Open **Settings** and set **your own** AI key, strategy and settings. Nothing
   is shared with your friend.

## 4. Trading

- You place and manage your trades exactly as the course teaches. All orders go
  through **your** NinjaTrader **SIM** account.
- If the dashboard says the add-on is disconnected, check NinjaTrader is running
  and its connection status is green.

## 5. Updates

- When a VL update is available you may see "VL update available — **Update now**
  / Later". It never updates while you are in a trade, and it never changes your
  settings or keys.

## Need help?

Ask your friend (the box owner). They can pause, resume or remove your access
from their **Friends** page.
