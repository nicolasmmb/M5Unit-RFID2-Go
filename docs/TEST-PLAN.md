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

## 2. `SetAntennaGain` / `GetAntennaGain` — ✅ FEITO (2026-11-08)

**EU**: exponho `SetAntennaGain(gain)` e `GetAntennaGain()` na lib (bits 6:4 do
`RFCfgReg`), com teste de host.
**VOCÊ**: encosta o cartão; eu meço o alcance com ganho mínimo vs máximo
(afastando o cartão em passos de 5 mm).
**RISCO**: zero (só registrador).
**Prova (obtida)**: o registrador grava e lê de volta (`Gain18dB` → `48` em
`RFCfgReg`), mas o **alcance medido não mudou**: ~3 dedos com 18 dB e ~3 dedos
com 48 dB. No ganho máximo o leitor ficou mais sensível a ruído (exigia mais
proximidade para leitura estável), o que é o comportamento esperado quando o
limitante é a antena e não o ganho do receptor.

**Medição em pares (18 dB e 48 dB quase no mesmo ponto, 1 par a cada ~4 s):**

| Posição do cartão | 18 dB | 48 dB |
|---|---|---|
| dentro da zona de energia (pares 1–12) | 100% | 100% |
| na borda (par 13) | **29%** | **0%** |
| acima da borda (pares 14–18) | 0% | 0% |

Conclusão: `SetAntennaGain` funciona no registrador (paridade com
`PCD_SetAntennaGain`), mas **não aumenta o alcance** — quem define é a energia
que chega no cartão. Na borda, **menos ganho foi melhor** (29% vs 0%): 48 dB
amplifica ruído. Valor prático: **38 dB**.

Ressalva de método: em cada par o 18 dB foi medido primeiro; movimento manual
dentro do par distorce a comparação (o par 13 pode ter parte disso).

## 3. Utilitários (`GetTypeName`, dump) — ✅ FEITO (2026-11-08)

**EU**: `Card.Type()` a partir do SAK/ATQA (Classic 1K/4K, Ultralight, NTAG) e
`ReadBlock`/`String` helpers, com testes de host usando SAK/ATQA conhecidos.
**VOCÊ**: nada.
**RISCO**: zero.
**Prova (obtida)**: `Card.Type()` com a tabela do `PICC_GetType`, testada
contra o cartão real e com 11 casos de host. **Comparado com o C++**: os dois
lados imprimem `type_code=4` para o mesmo cartão; só o texto difere
(`MIFARE 1KB` na oficial, `MIFARE Classic 1K` aqui).

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

## 6. Anticolisão com dois cartões — ✅ FEITO (2026-11-08)

**EU**: harness que reporta quantos cartões responderam e o UID de cada um.
**VOCÊ**: encostar **dois cartões ao mesmo tempo** na bobina.
**RISCO**: zero (só leitura).
**Prova (obtida)**: port bit a bit do `PICC_Select` (lê `CollReg`, escolhe o bit
= 1) + `SelectAll` com retry e dedupe. Medido no hardware:

| Cartões | Resultado |
|---|---|
| 1 | `UID e0de9a7f`, auth e leitura ok (sem regressão) |
| 2 | `distinct=2 [e0de9a7f] [0aee8d02]` na maioria das passadas |
| 3 | `distinct=3 [0aee8d02] [39819102] [e0de9a7f]` em algumas passadas |

Com cartões empilhados as respostas são intermitentes (dividem a energia do
campo), então o resultado depende da passada — documentado no README.

## 7. `SelectAll` (multi-cartão) — ✅ FEITO (2026-11-08)

**EU**: seção `SelectAll()` que devolve todos os cartões em campo.
**VOCÊ**: os dois cartões do item 6.
**RISCO**: zero.

## 8. Robustez (sem cartão) — ✅ FEITO (2026-11-08)

**EU**: testes de host para timeout, NACK, `ErrorReg` com bits fatais, cartão
que sai do campo no meio do auth, e `WriteBlock` recusado por bloco inválido.
Teste de estresse: 1000 selects seguidos sem vazar (heap estável).
**VOCÊ**: nada.
**RISCO**: zero.

## 9. Medições de tempo no alvo — ✅ FEITO (2026-11-08)

**EU**: `examples/timing` que imprime ms de `Select`, `Authenticate` e
`ReadBlock`.
**VOCÊ**: encosta o cartão uma vez.
**Prova (obtida)**: 100 kHz → `Select` 28,5 ms, `Auth` 4,7 ms, `Read` 9,1 ms.
400 kHz → **11,1 / 3,1 / 5,1 ms**, ciclo de ~19,3 ms (~50 leituras/s). O unit
**funciona a 400 kHz**, então a recomendação é 400 kHz. Números no BENCHMARKS.

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
