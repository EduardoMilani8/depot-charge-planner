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

## Formato

- Arquivos CSV em UTF-8. Separador `,` ou `;` (Excel brasileiro gera `;` com vírgula decimal, e isso é aceito).
- Datas e horas locais, sem fuso: `AAAA-MM-DD HH:MM` (também aceitamos `DD/MM/AAAA HH:MM`).
- Percentuais de carga em 0 a 100 (`soc_chegada_pct`, `soc_saida_exigido_pct`, `soc_saida_real_pct`).
- Uma "noite" é identificada pela chegada menos 12 horas (chegadas de 12:00 a 23:59 pertencem ao dia; de 00:00 a 11:59, ao dia anterior).
- Cabeçalhos exatamente como nos modelos; colunas extras são ignoradas.

## Cuidados com privacidade (LGPD)

- Não são necessários dados pessoais. Peça **sem** nome de motorista, placa ou matrícula; ônibus e carregadores podem ter códigos anonimizados.
- Combine por escrito para que fim os dados serão usados e quem tem acesso (peça à operadora se ela exige acordo de confidencialidade).
- Não coloque dados reais neste repositório: ele é público no GitHub. Guarde-os fora da pasta do projeto.

## O que este kit não resolve

- A premissa de que os operadores seguem os rodízios continua sendo suposição; o relatório mostra o planejador com e sem rodízios.
- Se a operadora só tiver o SoC de saída planejado e não o real, dá para comparar planejador × referências, mas não "real × planejador".
