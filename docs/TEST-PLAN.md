# Plano de teste do que falta

Estado: `Select`, `Authenticate` (chave A) e `ReadBlock` validados no hardware
(MIFARE Classic 1K, UID `e0de9a7f`). Paridade com o C++ medida.
Falta o que está abaixo, em ordem de valor.

Legenda: **EU** = eu faço sozinho · **VOCÊ** = precisa de ação física ·
**RISCO** = pode alterar/perder dados do cartão.

---

## 1. `WriteBlock` — ✅ FEITO (2026-11-08)

**EU**: estendo o harness com escrita + leitura de volta nos dois lados
(C++ e TinyGo), no **bloco 4** (primeiro bloco de dados do setor 1), e comparo.
**VOCÊ**: encosta o cartão quando eu pedir (2×, uma por firmware).
**RISCO**: baixo. Escrevo só no bloco 4; **nunca** no bloco 0 (manufacturer)
nem nos trailers (3, 7, 11…) — trailer errado pode travar o cartão.
**Prova (obtida)**: escrita em **duas fases** (a de uma fase deixava o cartão
mudo), `write=ok`, `readback` igual ao padrão, `verify=PASS`, e o conteúdo
original restaurado com `restore=PASS`.

**Nota**: o lado C++ não serviu de referência aqui porque a lib oficial **falha
no auth** neste cartão clone; a referência foi o protocolo MIFARE (duas fases).

## 2. `SetAntennaGain` / `GetAntennaGain`

**EU**: exponho `SetAntennaGain(gain)` e `GetAntennaGain()` na lib (bits 6:4 do
`RFCfgReg`), com teste de host.
**VOCÊ**: encosta o cartão; eu meço o alcance com ganho mínimo vs máximo
(afastando o cartão em passos de 5 mm).
**RISCO**: zero (só registrador).
**Prova**: leitura de volta do registrador e diferença de alcance.

## 3. Utilitários (`GetTypeName`, dump) — ✅ FEITO (2026-11-08)

**EU**: `Card.Type()` a partir do SAK/ATQA (Classic 1K/4K, Ultralight, NTAG) e
`ReadBlock`/`String` helpers, com testes de host usando SAK/ATQA conhecidos.
**VOCÊ**: nada.
**RISCO**: zero.
**Prova (obtida)**: `Card.Type()` com a tabela do `PICC_GetType`, testada
contra o cartão real (SAK 0x08 → MIFARE Classic 1K) e com 11 casos de host.

## 4. Ultralight page read/write

**EU**: implemento `ReadPage`/`WritePage` (comandos 0x30/0xA2) e `WritePage`
protegido por verificação.
**VOCÊ**: precisa de um **MIFARE Ultralight físico** (o MIFARE Classic não serve;
a emulação do Flipper só serve para `Select`, não para escrita).
**RISCO**: médio. Ultralight tem páginas de configuração (2, 3, 0x28-0x2B) que,
escritas erradas, **travam o cartão para sempre**. Vou escrever só na página 4
e só com o seu OK explícito.
**Prova**: `readPage(4)` antes/depois, e o OTP/config intocados.

## 5. Value blocks (Decrement/Increment/Restore/Transfer)

**EU**: implemento os 4 comandos e um `FormatValueBlock`.
**VOCÊ**: autorizar escrever um bloco de valor no seu cartão (bloco 5, por
exemplo) e depois eu restauro o conteúdo original.
**RISCO**: médio-alto — value block mal formado pode ser lido como transporte
e corromper a lógica do bloco (dados, não o cartão).
**Prova**: `Decrement` reduz o saldo, `Restore` volta, `ReadBlock` mostra o
formato (valor + complemento + backup).

## 6. Anticolisão com dois cartões

**EU**: harness que reporta quantos cartões responderam e o UID de cada um.
**VOCÊ**: encostar **dois cartões ao mesmo tempo** na bobina.
**RISCO**: zero (só leitura).
**Prova**: dois UIDs distintos, sem `ErrCollision`.

## 7. `PICC_Select` público / multi-cartão na API

**EU**: seção `SelectAll()` que devolve todos os cartões em campo.
**VOCÊ**: os dois cartões do item 6.
**RISCO**: zero.

## 8. Robustez (sem cartão) — ✅ FEITO (2026-11-08)

**EU**: testes de host para timeout, NACK, `ErrorReg` com bits fatais, cartão
que sai do campo no meio do auth, e `WriteBlock` recusado por bloco inválido.
Teste de estresse: 1000 selects seguidos sem vazar (heap estável).
**VOCÊ**: nada.
**RISCO**: zero.

## 9. Medições de tempo no alvo

**EU**: `examples/timing` que imprime ms de `Select`, `Authenticate` e
`ReadBlock`.
**VOCÊ**: encosta o cartão uma vez.
**Prova**: números reais para o BENCHMARKS (hoje só há host).

## 10. Documentação da placa — ✅ FEITO (2026-11-08)

**EU**: corrigir o README: a placa de teste é **StickC Plus v1** (AXP192), não
Plus2, e o unit é o **RFID2 (U031-B)**.
**VOCÊ**: nada.

---

## O que eu preciso de você (resumo)

| # | Ação | Quando |
|---|---|---|
| 1 | Encostar o MIFARE Classic `e0de9a7f` e **autorizar escrita no bloco 4** | itens 1 e 9 |
| 2 | Encostar o cartão para medir alcance (ganho mín/máx) | item 2 |
| 3 | Conseguir um **MIFARE Ultralight físico** | item 4 |
| 4 | Autorizar escrita de value block no bloco 5 | item 5 |
| 5 | Encostar **dois cartões juntos** | itens 6 e 7 |

Itens 3, 4, 8 e 10 eu faço sem você. Se você só puder fazer uma coisa:
**item 1** (fecha a última função de dados sem validação).
