# Paridade com a lib oficial C++ (MFRC522_I2C)

C++ é a fonte da verdade. Os dois lados rodam o **mesmo harness** (mesmas
chaves, mesma ordem, mesmo formato `chave=valor`) e os logs são comparados.

- C++: `M5-ProductExampleCodes/Unit/RFID/Arduino/RFID/MFRC522_I2C.{cpp,h}`,
  compilado com PlatformIO (`board = m5stick-c`), só `Wire` + `Serial`.
- TinyGo: `examples/parity` deste repo.
- Mesmo unit (WS1850S em 0x28), mesmo cartão: **MIFARE Classic 1K, UID `e0de9a7f`**.

## Resultado (lado a lado)

| Check | C++ oficial | TinyGo | Paridade |
|---|---|---|---|
| `version` | `15` | `15` | ✅ |
| `txcontrol` | `83` | `83` | ✅ |
| `antenna_gain` (RFCfgReg 7:4) | `40` | `40` | ✅ |
| `crc_3004` | `26ee` (coprocessador do chip) | `26ee` (software) | ✅ |
| `reqa_atqa` | `0400` | `0400` | ✅ |
| `select_uid` | `e0de9a7f` | `e0de9a7f` | ✅ |
| `select_sak` | `08` | `08` | ✅ |
| `auth0` | fail | **ok** | ⚠️ diverge |
| `read0` | — | `e0de9a7fdb080400475955d141103607` | ⚠️ só Go leu |
| `auth4` / `auth8` | fail | **ok** | ⚠️ diverge |
| `read4` / `read8` | — | `0000…00` | ⚠️ só Go leu |
| `stopcrypto` | ok | ok | ✅ |
| `halt` | `fail` (status ≠ 0) | `ok` | ⚠️ semântica |

## As duas divergências

1. **`auth`**: a lib oficial falha a autenticação com chave A `FF..FF` neste
   cartão; a nossa passa e lê os blocos 0, 4 e 8. O bloco 0 confere com o
   manufacturer block do cartão (`e0de9a7f db 08 0400 475955d141103607`), então
   a leitura é real, não lixo. Causa provável: o cartão é um clone e a oficial
   faz uma checagem a mais (`ErrorReg & 0x13` ou o `validBits` mal propagado —
   o mesmo defeito que faz `PICC_RequestA` retornar `STATUS_ERROR` com ATQA
   válido).
2. **`halt`**: a oficial retorna o status do transceive, que é **não-zero**
   porque o HALT não tem resposta (só `IdleIRq`). A nossa retorna `nil` nesse
   caso, porque `IdleIRq` significa "comando concluído". Comportamento no fio é
   o mesmo; a diferença é o que se reporta.

## Tipo do cartão (`PICC_GetType` / `PICC_GetTypeName`)

Mesmo cartão (`e0de9a7f`, SAK 0x08), dois lados:

| Campo | C++ oficial | TinyGo | |
|---|---|---|---|
| `type_code` | `4` | `4` | ✅ enum idêntico |
| `type_name` | `MIFARE 1KB` | `MIFARE Classic 1K` | ⚠️ só o texto |

Os valores numéricos de `Type` foram alinhados com `PICC_Type` do C++
(`Unknown 0`, `ISO14443_4 1`, `ISO18092 2`, `Mini 3`, `1K 4`, `4K 5`,
`UL 6`, `Plus 7`, `TNP3XXX 8`, `NotComplete 255`), então logs e fixtures
comparáveis por número. O texto ficou o nosso, mais legível; a comparação
confiável é pelo código.

## Quirks da lib oficial encontrados no caminho

| Quirk | Efeito | Como tratamos |
|---|---|---|
| `PICC_RequestA` devolve `STATUS_ERROR` mesmo com ATQA válido | `IsNewCardPresent()` pode parecer falha | `Select` não depende do status |
| `PICC_HaltA` devolve status não-zero em sucesso | `halt=fail` enganoso | `Halt` não falha por ausência de resposta |

## Matriz da API

| C++ (`MFRC522`) | Nossa lib | Estado |
|---|---|---|
| `PCD_Init`, `PCD_Reset` | `Init`, `Reset` | ✅ validado |
| `PCD_AntennaOn/Off` | `Antenna(bool)` | ✅ validado |
| `PCD_ReadRegister` / `PCD_WriteRegister` | `Register` / `SetRegister` | ✅ validado |
| `PCD_SetRegisterBitMask` (RMW) | `setBits` (interno) | ✅ mesmo comportamento |
| `PCD_CalculateCRC` (chip) | `crcA` (software) | ✅ valores idênticos |
| `PCD_CommunicateWithPICC` | `doTransceive` (interno) | ✅ mesma sequência |
| `PCD_TransceiveData` | `Transceive` | ✅ |
| `PCD_MIFARE_Transceive` | parte de `doTransceive` + `transceiveBits` | ✅ |
| `PICC_REQA_or_WUPA` / `PICC_RequestA` / `PICC_WakeupA` | dentro de `Select` (REQA + fallback WUPA) | ✅ validado |
| `PICC_Select`, `PICC_ReadCardSerial`, cascata 1–3 | `Select` → `Card` | ✅ validado (cascata 2 coberta por teste de host) |
| `PICC_HaltA` | `Halt` | ✅ validado |
| `PICC_IsNewCardPresent` | `Select` (retorna erro `ErrNoCard`/`ErrTimeout`) | ✅ |
| `PCD_Authenticate` (A e B) | `Authenticate` | ✅ chave A validada |
| `MIFARE_Read` | `ReadBlock` | ✅ validado |
| `MIFARE_Write` | `WriteBlock` | ✅ validado em cartão (escrita em duas fases) |
| `PCD_StopCrypto1` | `StopCrypto1` | ✅ validado |
| `PCD_GetAntennaGain` | `Register(0x26) & 0x70` | ⚠️ só leitura, sem setter |
| `PCD_SetAntennaGain` | — | ❌ falta |
| `MIFARE_Decrement` / `Increment` / `Restore` / `Transfer` | — | ❌ falta (value blocks) |
| `MIFARE_Ultralight_ReadPage` / `WritePage` | — | ❌ falta |
| `PICC_GetType` | `Card.Type()` (mesmos valores numéricos) | ✅ validado |
| `PICC_GetTypeName` | `Type.String()` | ✅ (texto diferente, código igual) |
| `PICC_Dump*`, `PCD_DumpVersionToSerial` | — | ❌ falta |
| `PICC_Select` público (multi-cartão) | — | ❌ falta |

## `WriteBlock`: duas fases

O MIFARE Classic escreve em **duas fases**: `A0 bloco CRC` → ACK de 4 bits, e
depois `16 bytes CRC` → segundo ACK. Mandar os 20 bytes num quadro só (como faz
a lib C oficial) deixa o cartão esperando a segunda fase e ele **para de
responder** — foi o que aconteceu no primeiro teste de escrita.

Medido no hardware (bloco 4, restaurado depois):

```
auth=ok
original=00000000000000000000000000000000
write=ok
readback=54494e59474f2d57524954452d4f4b21   ("TINYGO-WRITE-OK!")
verify=PASS
restored=00000000000000000000000000000000
restore=PASS
```

## Próximos passos de paridade

1. ~~`WriteBlock` num bloco de dados (4) com leitura de volta~~ ✅
2. `SetAntennaGain` + `GetAntennaGain` na API.
3. Value blocks (`Decrement`/`Increment`/`Restore`/`Transfer`) se algum projeto
   usar carteira MIFARE.
4. Ultralight page read/write (precisa de um cartão Ultralight).
