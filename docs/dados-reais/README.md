# Kit de dados reais

Este kit serve para pedir a uma operadora os dados de uma ou mais noites de uma garagem e rodar o planejador sobre eles (passo 1 do roadmap: `docs/superpowers/plans/2026-10-08-proximos-passos.md`; especificação: `docs/superpowers/specs/2026-10-08-validacao-dados-reais-design.md`).

Os arquivos em `modelos/` são **fictícios**, só mostram o formato. Nada neste kit foi medido numa garagem real.

## Texto para enviar à operadora (adapte)

> Estamos desenvolvendo um sistema que decide a potência de cada carregador numa garagem de ônibus elétricos, para que os ônibus saiam carregados dentro do limite de potência da rede. Para testar o sistema com dados reais, sem controlar nada na garagem, gostaríamos de planilhas de **algumas noites** (quanto mais, melhor; 10 a 30 já ajudam muito). Podemos receber os dados com os ônibus anonimizados (por exemplo, B01, B02…). Os dados ficam só no computador de quem roda a análise e não serão publicados nem compartilhados. Resultado: um relatório comparando o que aconteceu com o que o sistema teria feito.

## O que pedir

**Versão mínima** (já permite comparar prontidão):

1. `garagem.csv`: potência disponível para a garagem (`limite_kw`).
2. `carregadores.csv`: um por carregador (`carregador_id`, `potencia_max_kw`).
3. `onibus.csv`: um por ônibus e por noite: `onibus_id`, `capacidade_kwh`, `chegada`, `soc_chegada_pct`, `saida_prevista`, `soc_saida_exigido_pct` (o quanto a escala exige de carga na saída).

**Versão ideal** (acrescenta energia, pico e custo e o desfecho real):

4. Em `onibus.csv`: `soc_saida_real_pct` e `saida_real` (carga e hora em que o ônibus de fato saiu).
5. `sessoes.csv`: cada sessão de carga (`onibus_id`, `carregador_id`, `inicio`, `fim`, `energia_kwh` do medidor do carregador).
6. `potencia.csv`: potência de cada carregador a cada 1 a 15 minutos (`instante`, `carregador_id`, `potencia_kw`).
7. Em `garagem.csv`: janela de ponta e preços (R$/kWh) para calcular custo.

## Como rodar

Ponha as planilhas numa pasta **fora do repositório** com estes nomes exatos (os três primeiros são obrigatórios; os outros dois são opcionais e acrescentam energia, pico e custo):

| arquivo | obrigatório | conteúdo |
|---|---|---|
| `garagem.csv` | sim | limite de potência e, se houver, janela de ponta e preços |
| `carregadores.csv` | sim | um carregador por linha |
| `onibus.csv` | sim | um ônibus por noite por linha |
| `sessoes.csv` | não | sessões de carga (energia do medidor) |
| `potencia.csv` | não | potência de cada carregador ao longo da noite |

Na linha de comando:

    go run ./cmd/replay -dir /caminho/da/pasta

Opções: `-soc-noise KWH` (ruído nas leituras de carga), `-swap-back-cooldown MIN`, `-swap-back-min-need KWH`, `-json` e `-workers N`; a descrição de cada uma e um trecho da saída estão na seção "Validação com dados reais" do `README.md` da raiz. Para ver o formato funcionando, use o exemplo fictício completo `go run ./cmd/replay -dir internal/realdata/testdata/demo`; os modelos de `modelos/` têm uma noite só e pouca informação (`go run ./cmd/replay -dir docs/dados-reais/modelos` funciona, mas é só um teste de formato).

No laboratório web (`go run ./cmd/lab`), a aba "Dados reais" faz a mesma comparação: escolha os arquivos (ou a pasta) ou arraste para a área. Os arquivos ficam no seu computador: o navegador fala só com o laboratório local, que não grava nada em disco, e os dados não vão no link da tela. A execução detalhada de uma noite nessa aba ignora a opção de ruído.

## Formato

- Arquivos CSV em UTF-8. Separador `,` ou `;` (Excel brasileiro gera `;` com vírgula decimal, e isso é aceito).
- Datas e horas locais, sem fuso: `AAAA-MM-DD HH:MM` (também aceitamos `DD/MM/AAAA HH:MM`, com o ano em 4 dígitos). Não aceitamos `Z`, `+00:00` nem ano com 2 dígitos.
- Percentuais de carga em 0 a 100 (`soc_chegada_pct`, `soc_saida_exigido_pct`, `soc_saida_real_pct`); uma célula como `30%` também é aceita. Uma coluna cujos valores são todos 0 a 1 (frações, como `0.30` para 30%) é recusada, porque cada ônibus ficaria com uma necessidade de poucos kWh: multiplique por 100.
- Arquivos em UTF-16 (o "Unicode" do Excel) não são aceitos: salve como CSV UTF-8.
- Uma "noite" é identificada pela chegada menos 12 horas (chegadas de 12:00 a 23:59 pertencem ao dia; de 00:00 a 11:59, ao dia anterior). As sessões e as leituras de potência são atribuídas à noite pelo mesmo critério, aplicado ao início da sessão e ao instante da leitura (veja "Limitações dos números").
- Cabeçalhos exatamente como nos modelos; colunas extras são ignoradas.

## Limites aceitos

Valores fora destes limites são recusados com o arquivo, a linha e a coluna do problema (quase sempre é erro de digitação ou de unidade):

- Cada arquivo: até 8 MB, até 64 colunas e até 1.000.000 de linhas de dados.
- Capacidade do ônibus: 1 a 2000 kWh. Potência máxima do carregador: mais de 0 e até 5000 kW. Potência máxima da bateria: mais de 0 e até 5000 kW.
- Permanência do ônibus (`saida_prevista` e `saida_real` menos `chegada`): até 48 horas. Duração de uma sessão (`fim` menos `inicio`): até 48 horas.
- Energia de uma sessão (`energia_kwh`): até 1.000.000 kWh. Potência de uma leitura (`potencia_kw`): até 100.000 kW (leitura acima de 2 vezes a potência máxima do carregador só gera aviso).
- Até 1000 ônibus por noite, 366 noites e 10.000 ônibus ou carregadores no total.
- Uma noite que dura mais de 3000 minutos (da hora cheia anterior à primeira chegada, menos 1 hora, até a última saída prevista ou real mais 30 minutos) é omitida com um aviso: confira as datas de saída.
- No laboratório web, cada requisição tem limite de 60 segundos e o corpo, de 16 MB. Uma comparação de 10.000 ônibus levou 19 s em 16 núcleos; num notebook pequeno pode passar de 60 s e dar erro de tempo esgotado. Para conjuntos grandes use a linha de comando (`replay`), que não tem esse limite.

## Limitações dos números

Leia isto antes de tirar conclusões de um relatório:

- **Qual noite recebe cada dado.** A noite de um ônibus é a da chegada menos 12 horas. Cada sessão e cada leitura de potência vai para a noite do seu início menos 12 horas (instante menos 12 horas, no caso da leitura). Por isso uma leitura depois das 12:00 do dia da saída cai na **noite seguinte**, e uma leitura numa noite sem nenhum ônibus em `onibus.csv` é ignorada com um aviso. Mantenha cada arquivo dentro das janelas das noites que quer comparar.
- **Leituras de potência e o pico medido.** Cada leitura vale até a próxima leitura do mesmo carregador; a última leitura de cada carregador vale 0 minutos de energia, mas, no **pico medido**, a última leitura diferente de zero de cada carregador continua somada até o fim da noite. Se um arquivo termina sem uma leitura final em zero, o pico pode ficar superestimado. **Termine a série de cada carregador com uma leitura 0.** Se `potencia.csv` cobre só parte da noite, o pico e o custo reais (calculados dele) refletem só essas leituras; a energia continua vindo das sessões quando elas existem. Cada número usa uma única fonte na noite, sem misturar sessões e leituras.
- **Cobertura parcial (`parcial: X de Y ônibus`).** A energia, o pico e o custo reais vêm de `sessoes.csv` e `potencia.csv`, que podem não cobrir todos os ônibus da noite. Se `sessoes.csv` não tem nenhuma sessão para alguns ônibus da noite (ou para a noite inteira, quando o arquivo tem sessões de outras noites), ou se a energia das sessões e a integral das leituras de potência diferem em mais de 20%, a noite é marcada como **parcial**: o valor continua na tabela (não é apagado), mas leva o selo `(parcial: 2 de 3 ônibus)` na tela e na linha de comando, e um aviso diz o que falta. Esse número é de parte da noite e **não deve ser comparado** com as linhas simuladas, que cobrem todos os ônibus. No agregado, o selo aparece se qualquer noite é parcial e conta os ônibus cobertos sobre o total. Sem `sessoes.csv` nenhuma noite é marcada: o relatório não tem como saber que algo falta.
- **Pico e custo "estimado".** Sem leituras de potência na noite, o pico e o custo vêm das sessões, e a energia de cada sessão é espalhada de modo uniforme entre o início e o fim. A potência de uma sessão real não é constante (cai no fim da carga), então o pico estimado costuma ser menor que o real. O relatório marca esses valores com `(estimado)`.
- **Janela de ponta que atravessa a meia-noite** (por exemplo 22:00 a 06:00). O simulador só aceita janelas dentro do mesmo dia e trata o horário como fora de ponta; por isso o custo simulado não é comparável com o custo real e aparece como `—` nas linhas simuladas. O custo real continua calculado com a janela correta.
- **Potência máxima da bateria.** Se `potencia_max_bateria_kw` não vem na planilha, vale a potência de carregador mais comum em `carregadores.csv` (em empate, a maior).
- **Definição de "pronto".** O ônibus saiu pronto se a carga real na saída (`soc_saida_real_pct`) é pelo menos a exigida menos 1e-6 kWh, a mesma regra do simulador. A necessidade nunca passa da capacidade. O `ready%` de uma noite é a porcentagem dos ônibus **com** `soc_saida_real_pct`; o `ready%` agregado é a **média** dos percentuais de cada noite (não o total de ônibus prontos sobre o total de ônibus). Ônibus sem resultado real nunca contam como não prontos; se nenhum ônibus da noite tem `soc_saida_real_pct`, o relatório diz que o resultado real é desconhecido.
- **Comparação justa (mesmos ônibus).** As linhas simuladas cobrem todos os ônibus da noite, mas o `ready%` e o `shortfall` reais só os que têm `soc_saida_real_pct`. Por isso, sob cada tabela, a linha "Comparação justa (mesmos N ônibus do real)" mostra `ready%` e `shortfall` do `planner` e do `planner (sem rodízio)` só para esses N ônibus: é essa a linha para comparar com o `real`. No agregado, N é a soma dos ônibus e os percentuais são médias das noites que têm pelo menos um desses ônibus.
- **Saída real atrasada.** Um ônibus cuja `saida_real` é mais de 15 minutos depois da `saida_prevista` conta como pronto se a carga estava completa na saída (a simulação o tira no horário previsto). O relatório avisa, por noite, quantos foram: o "pronto" desses pode dever-se ao tempo extra ligado.
- **Agregado.** Nas linhas de todas as noites, a maioria das colunas é a média por noite, mas `plan violations` e `overshoot min` são somas das noites e `plan changes` é a média arredondada para baixo.
- **A comparação é contrafactual.** O simulador reproduz as condições da noite (chegadas, saídas, cargas, limite), mas a operação real tomou decisões (ordem de ligação, rodízios manuais) que o planejador não vê; o limite da garagem é fixo e as leituras de carga são perfeitas, a menos que se use `-soc-noise`.

## Cuidados com privacidade (LGPD)

- Não são necessários dados pessoais. Peça **sem** nome de motorista, placa ou matrícula; ônibus e carregadores podem ter códigos anonimizados.
- Combine por escrito para que fim os dados serão usados e quem tem acesso (peça à operadora se ela exige acordo de confidencialidade).
- Não coloque dados reais neste repositório: ele é público no GitHub. Guarde-os fora da pasta do projeto.

## Onde procurar (pesquisa de 2026-10-08)

Não encontrei, até agora, nenhum conjunto **público** com ônibus por ônibus numa garagem (chegada, saída, carga e potência). O que apareceu:

- Perfis de demanda de garagens de ônibus elétricos dos EUA, modelados e por hora (NREL, `data.nrel.gov`, DOI 10.7799/2565433) e estatísticas nacionais de operação de garagens (catálogo `data.gov`): servem para comparar a **forma** da curva de carga, não para testar o planejador ônibus a ônibus.
- Relatórios de avaliação de frotas de ônibus elétricos nos EUA (NREL/FTA, por exemplo King County Metro): dados agregados e quase sempre só em PDF.
- Conjunto do projeto OPTIMISE Prime (Londres, licença Creative Commons Attribution), com dados de carregadores de depósitos: a página fala de veículos comerciais e de aplicativo, não confirma ônibus. Vale abrir a documentação antes de descartar.
- Um conjunto no Mendeley Data da Universidade de Coimbra sobre carga inteligente de frotas de ônibus elétricos (CC BY 4.0). Não consegui abrir o conteúdo; pode ser entrada de um estudo de otimização, o que ainda ajuda para criar cenários mais realistas.

Conclusão prática: o caminho mais provável é **pedir por e-mail**. Quem costuma ter esse dado: operadoras de ônibus elétricos (em São Paulo, as concessionárias contratadas pela SPTrans), prefeituras e empresas de transporte público nos EUA e na Europa que publicam relatórios de projeto, e fabricantes de ônibus e de carregadores (que têm telemetria). Universidades que escreveram sobre o tema também costumam compartilhar dados mediante pedido ao autor.

## O que este kit não resolve

- A premissa de que os operadores seguem os rodízios continua sendo suposição; o relatório mostra o planejador com e sem rodízios.
- Se a operadora só tiver o SoC de saída planejado e não o real, dá para comparar planejador × referências, mas não "real × planejador".
