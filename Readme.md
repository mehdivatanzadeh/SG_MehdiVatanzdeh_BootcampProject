# GhostOps

یه ابزار خط فرمان با زبان Go که فایل لاگ رو به‌صورت streaming می‌خونه، آمار می‌گیره، incident ها رو تشخیص می‌ده و می‌تونه گزارش JSON خروجی بده. فقط از standard library استفاده شده.

## ساخت و اجرا

```
go build -o ghostops .
./ghostops [-o report.json] <logfile>
```

- بدون `-o`: گزارش متنی تو ترمینال چاپ می‌شه.
- با `-o`: گزارش متنی چاپ می‌شه **و** گزارش JSON هم تو فایل داده‌شده نوشته می‌شه.
- پرچم باید **قبل از** مسیر فایل لاگ بیاد (پکیج `flag` تو Go از اولین آرگومان معمولی به بعد پارس نمی‌کنه).

تست‌ها با `go test ./...` اجرا می‌شن. فایل نمونه تو `testdata/sample.log` هست.

Exit code ها: `0` موفق، `1` خطای اجرا (خطای فایل یا خروجی)، `2` آرگومان اشتباه.

## فرمت لاگ

```
<RFC3339 timestamp> <LEVEL> <service> <message...>
2026-10-26T14:03:22Z ERROR auth-service Database connection timeout
```

سطح‌ها: `INFO`، `WARN`، `ERROR`، `FATAL` (فقط با حروف بزرگ). هر چیزی بعد از نام سرویس، پیام حساب می‌شه.
یه خط **malformed** حساب می‌شه اگه کمتر از ۴ فیلد داشته باشه، timestamp اشتباه باشه، level ناشناخته باشه یا خالی باشه. خط‌های malformed شمرده و رد می‌شن.

## قانون‌های incident

قانون‌ها به ترتیب اولویت چک می‌شن و **اولین match برنده‌ست**، پس هر entry حداکثر تو یه incident شمرده می‌شه.

| Incident           | شرط                                         | شدت      |
| ------------------ | ------------------------------------------- | -------- |
| Fatal error        | level برابر `FATAL`                         | CRITICAL |
| Connection failure | `ERROR`/`WARN` که `connection refused` داره | HIGH     |
| Timeout            | `ERROR`/`WARN` که `timeout` داره            | HIGH     |
| Retry storm        | `ERROR`/`WARN` که `retry` داره              | MEDIUM   |

جستجوی کلمه‌ها به حروف بزرگ و کوچیک حساس نیست. entry ها بر اساس (نوع incident، سرویس) گروه می‌شن و هر گروه تعداد، شدت، اولین و آخرین زمان رو نگه می‌داره.
incident ها بر اساس شدت (زیاد به کم)، بعد اولین زمان، بعد نوع و بعد سرویس مرتب می‌شن.

## نمونه خروجی

`./ghostops -o report.json testdata/sample.log`

```
GhostOps Report
===============
Valid entries: 11
Malformed lines: 3

By level:
  INFO: 2
  WARN: 4
  ERROR: 3
  FATAL: 2

By service:
  api-gateway: 5
  auth-service: 4
  payment-service: 2

Incidents (5):
  [CRITICAL] Fatal error | service=payment-service | count=1 | 2026-10-26T10:03:30Z -> 2026-10-26T10:03:30Z
  [CRITICAL] Fatal error | service=auth-service | count=1 | 2026-10-26T10:08:00Z -> 2026-10-26T10:08:00Z
  [HIGH] Timeout | service=auth-service | count=2 | 2026-10-26T10:01:10Z -> 2026-10-26T10:07:00Z
  [HIGH] Connection failure | service=payment-service | count=1 | 2026-10-26T10:02:00Z -> 2026-10-26T10:02:00Z
  [MEDIUM] Retry storm | service=api-gateway | count=3 | 2026-10-26T10:06:00Z -> 2026-10-26T10:06:10Z
```

فایل `report.json` (کوتاه‌شده تا اولین incident):

```json
{
  "valid_entries": 11,
  "malformed_lines": 3,
  "by_level": { "ERROR": 3, "FATAL": 2, "INFO": 2, "WARN": 4 },
  "by_service": { "api-gateway": 5, "auth-service": 4, "payment-service": 2 },
  "incidents": [
    {
      "type": "Fatal error",
      "severity": "CRITICAL",
      "service": "payment-service",
      "count": 1,
      "first_seen": "2026-10-26T10:03:30Z",
      "last_seen": "2026-10-26T10:03:30Z"
    }
  ]
}
```

فایل خالی یا فایلی که incident نداره هم گزارش معتبر می‌ده (`Incidents (0): none` و تو JSON مقدار `"incidents": []`).

## معماری

برنامه یه `package main` هست که به بخش‌های جدا تقسیم شده:

| بخش   | کد                                                | مسئولیت                                     |
| ----- | ------------------------------------------------- | ------------------------------------------- |
| CLI   | `main`                                            | آرگومان‌ها، صدا زدن بخش‌های دیگه، exit code |
| پارس  | `parseLine`, `Entry`                              | تبدیل یه خط به `Entry` یا برگردوندن خطا     |
| ورودی | `readLogFile`                                     | خوندن streaming فایل و پر کردن `Stats`      |
| تشخیص | `Rule`, `findRule`, `addIncident`                 | دسته‌بندی entry ها و گروه‌بندی incident ها  |
| گزارش | `buildReport`, `writeTextReport`, `writeJSONFile` | تبدیل `Stats` به متن یا JSON                |

## تصمیم‌های طراحی

- **Streaming.** فایل با `bufio.Scanner` خط‌به‌خط خونده می‌شه و هیچ entry ای نگه داشته نمی‌شه. مصرف حافظه فقط با تعداد سرویس‌های متفاوت و گروه‌های incident زیاد می‌شه، نه با اندازه‌ی فایل. بافر `Scanner` تا ۱ مگابایت بزرگ شده و `scanner.Err()` چک می‌شه، پس خطای خوندن یا خط بلندتر از ۱ مگابایت خطای اجراست و بی‌صدا نادیده گرفته نمی‌شه.
- **دو نوع خطا.** خط malformed رد و شمرده می‌شه. فایل ناموجود، خطای خوندن یا خطای نوشتن خروجی اجرا رو متوقف می‌کنه، پیام رو تو stderr می‌نویسه و exit code `1` می‌ده. `Close()` فایل JSON هم چک می‌شه.
- **فقط یه interface: `Rule`.** چون تنها جاییه که انتظار تغییر داریم (اضافه کردن قانون جدید بدون دست زدن به خوندن و گروه‌بندی). بقیه‌ی بخش‌ها تابع ساده‌ان.
- **قانون‌های هم‌پوشان.** لیست مرتب با اولین match: ساده، deterministic و بدون شمارش دوبل (مثلاً یه `FATAL` که کلمه‌ی timeout داره فقط Fatal error حساب می‌شه).
- **گزارش deterministic.** کلید map ها قبل از چاپ sort می‌شن، level ها با ترتیب ثابت چاپ می‌شن، incident ها ترتیب کامل دارن و `encoding/json` کلید map ها رو sort می‌کنه.
- **لاگ نامرتب.** اولین و آخرین زمان با مقایسه‌ی timestamp به‌روز می‌شن، نه با فرض ترتیب خط‌ها.
- **خروجی قابل تست.** `writeTextReport` یه `io.Writer` می‌گیره، پس تست‌ها خروجی رو تو یه buffer می‌گیرن.
