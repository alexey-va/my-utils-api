# VPN architecture, deployment, and recovery design

Этот документ описывает рабочую VPN-систему `my-utils` и является основной
точкой входа для агента, которому нужно понять, развернуть или восстановить её
целиком. Команды установки и сами host-скрипты находятся рядом в
[`README.md`](README.md) и [`ansible/`](ansible/README.md).

Снимок production-топологии в этом документе актуален на 2026-09-01. Публичные
IP, DNS, состояние провайдера и SSH-алиасы со временем меняются, поэтому перед
любой операцией их надо перечитать с живых хостов. Код, tracked-конфигурация и
Ansible важнее этого снимка, а живой data plane важнее зелёного systemd unit.

## 1. Назначение и границы

Система решает четыре задачи:

1. Принимает стандартные WireGuard-подключения пользователей на `utils`.
2. Отправляет российские IPv4-назначения напрямую через обычный интернет
   `utils`, а остальные — через один из двух независимых AmneziaWG exit-хостов.
3. Автоматически переключает внешний трафик между primary и secondary exit,
   не допуская утечки во внешний интернет через обычный маршрут `utils`.
4. Даёт API, UI, историю трафика, healthcheck, алерты и отдельный туннель для
   HTTP proxy с Velocity.

Не поддерживаются:

- IPv6-транзит;
- маршрутизация по доменному имени или принадлежности компании;
- защита пользовательского last mile;
- автоматическое изменение внешнего DNS;
- автоматическое восстановление утраченных ключей;
- публичный доступ к Tinyproxy.

## 2. Источники правды

| Область | Источник правды |
| --- | --- |
| Host installers и systemd units | этот каталог `ops/wireguard/` |
| Полная оркестрация трёх хостов | `ops/wireguard/ansible/site.yml` |
| Публичная топология | локальный, некоммитящийся `ansible/inventory.yml` |
| Stable private keys и agent token | зашифрованный `ansible/vault.yml` вне Git |
| Relay, peers, desired revision, метрики | PostgreSQL через `internal/wireguard/` |
| Админские и agent HTTP routes | `internal/httpapi/wireguard.go` |
| Production API topology | `docker-compose.jenkins.yml` |
| Alerts и dashboards | `observability/`, не соседний `utils/observability/` |
| Живой выбранный exit | `/var/lib/my-utils-wireguard/exit-health.json` |
| Живой режим маршрутизации | kernel rules/routes/nftables, не status UI |

Исторические документы в `docs/superpowers/` объясняют решения, но не
переопределяют текущий код.

Не править сгенерированные копии в `/usr/local/libexec` как единственный
источник изменения. Исправление сначала вносится в репозиторий, проверяется,
после чего соответствующий installer применяется контролируемо.

## 3. Топология

```text
                                       control plane
                         +--------------------------------------+
                         | my-utils API + PostgreSQL            |
                         | desired peers, keys, metrics, health |
                         +------------------^-------------------+
                                            | token-auth heartbeat / 15 s
                                            |
client -- standard WG/UDP 51820 --> utils: wg-users 10.89.0.1/24
                                            |
                 +--------------------------+--------------------------+
                 |                                                     |
        RU destination in nft set                            all other IPv4
        mark 0x51890, rule 1088                              rule 1089
                 |                                                     |
        main table -> eth0                                  table 51889
        MASQUERADE on utils                           selected default route
                 |                                  +---------+---------+
             RU internet                            |                   |
                                             awg-exit primary    awg-exit-b reserve
                                             10.8.1.250/32       10.8.2.250/32
                                                    |                   |
                                      UDP 42697     |                   | UDP 42697
                                                    v                   v
                                             exit primary         exit secondary
                                             AWG container        AWG container
                                             NAT to internet      NAT to internet
                                                    |                   |
                                                    +---- same logical-+
                                                         proxy address
                                                         172.29.172.3:8888
```

Отдельный путь Velocity:

```text
ProxyARC -> proxy selector 91.197.0.191:8888
    -> local DNAT on Velocity to 172.29.172.3:8888
    -> wg-utils 10.89.0.7/32
    -> utils wg-users
    -> currently selected AWG exit
    -> Tinyproxy 172.29.172.3:8888
```

### 3.1 Production snapshot

| Роль | Текущее значение | Стабильная идентичность |
| --- | --- | --- |
| Relay | `utils`, Ubuntu 24.04, private `10.130.0.32` | `utils.alexeyav.ru:51820` |
| Relay public IP | `51.250.112.232` на момент снимка | DNS, а не IP в client config |
| Ingress | `wg-users`, `10.89.0.1/24`, UDP `51820` | stable private key из Vault |
| Primary exit | `91.197.0.191:42697`, Ubuntu 26.04 | `awg-exit`, overlay `10.8.1.0/24` |
| Secondary exit | `153.76.223.117:42697`, Ubuntu 26.04 | `awg-exit-b`, overlay `10.8.2.0/24` |
| Velocity | `31.44.9.177`, Ubuntu 24.04 | `wg-utils`, address `10.89.0.7/32` |
| Tinyproxy | не публикуется на host | `172.29.172.3:8888` внутри каждого exit |

Локальный SSH alias `veesp` не является доказательством роли хоста: проверять
expected egress в
`/etc/my-utils/awg-failover.env` и реальный `curl https://api.ipify.org`.

## 4. Адреса, метки и порты

| Назначение | Значение |
| --- | --- |
| Client CIDR | `10.89.0.0/24` |
| Relay/DNS address | `10.89.0.1` |
| Primary AWG overlay | `10.8.1.0/24`, relay side `.250` |
| Secondary AWG overlay | `10.8.2.0/24`, relay side `.250` |
| Exit Docker bridge | `172.29.172.0/24`, bridge `amn0` |
| AWG container | `172.29.172.2` |
| Tinyproxy container | `172.29.172.3:8888` |
| Fail-closed routing table | `51889` |
| API proxy mark/rule | `0x51891`, priority `1087` |
| RU-direct mark/rule | `0x51890`, priority `1088` |
| Client source rule | priority `1089` |

Публичная матрица портов:

| Хост | Порт | Назначение |
| --- | --- | --- |
| Relay | UDP `51820` | standard WireGuard ingress |
| Relay | TCP `22`, `80`, `443` | SSH и web; не часть VPN forwarding |
| Каждый exit | UDP `42697` | AmneziaWG ingress |
| Каждый exit | TCP `22` | key-only SSH |
| Каждый exit | TCP `8888` | **не должен публиковаться** |

Docker может публиковать порт в обход ожидаемого поведения UFW. Проверка
`ufw status` недостаточна: смотреть `docker inspect ...PortBindings`, `ss` и
фактическую доступность снаружи. На текущих exit UFW выключен, key-only SSH и
fail2ban включены; это осознанно не заменяет provider firewall или DDoS-защиту.

## 5. Control plane и peer lifecycle

Администратор создаёт relay через UI или
`POST /api/admin/wireguard/relays`. Минимальный payload:

```json
{
  "name": "utils",
  "publicEndpoint": "utils.alexeyav.ru:51820",
  "clientCidr": "10.89.0.0/24",
  "clientDns": "10.89.0.1"
}
```

Ответ содержит relay UUID и plaintext `agentToken` ровно для enrolment. В БД
сохраняется только hash токена. Токен сразу кладётся в mode-600 файл или в
Ansible Vault; его нельзя восстановить, только ротировать.

`my-utils-wireguard-agent.timer` запускает oneshot agent каждые 15 секунд:

1. перед heartbeat восстанавливает пропавшие owned policy rules;
2. получает полный desired peer set по relay token;
3. валидирует interface, revision, public keys и уникальные `/32`;
4. применяет полный peer set через `wg syncconf`;
5. обновляет exit preference;
6. снимает handshake, counters, RU/External counters и route quality;
7. отправляет heartbeat в API.

Следствия:

- ручной peer, добавленный только в live `wg-users`, будет удалён следующим
  convergence cycle;
- API/БД недоступны — уже применённые peers и forwarding продолжают работать,
  но изменения и метрики не обновляются. После reboot relay базовый
  `wg-users.conf` поднимется без dynamic peers, и они вернутся только после
  успешного desired-state fetch;
- `wg-users.conf` содержит только interface identity; dynamic peers принадлежат
  control plane;
- `SaveConfig = false` обязателен, иначе runtime peers перезапишут базовый
  конфиг при shutdown;
- service может выглядеть `activating`, пока выполняет probes. Для диагностики
  смотреть `Type=oneshot`, `Result`, `ExecMainStatus` и journal, а не один
  моментальный `is-active`.

Client private keys хранятся в БД только как AES-256-GCM ciphertext. Ключ
`WIREGUARD_CREDENTIALS_ENCRYPTION_KEY` — base64 от ровно 32 bytes — является
отдельным production secret.

## 6. Data plane

### 6.1 Client DNS

`dnsmasq` слушает только `10.89.0.1` и loopback. Цепи
`MYUTILS-WG-DNS`/`MYUTILS-WG-DNS-IN` перехватывают TCP и UDP `53` только для
пакетов, пришедших с `wg-users` из `10.89.0.0/24`.

Поэтому старый профиль с `DNS = 1.1.1.1` фактически пользуется локальным
resolver без перевыпуска. Upstream DNS идёт через обычный main route `utils`,
не через AWG. Resolver не должен слушать публичный интерфейс.

### 6.2 RU-direct

`update-geo-routing.sh` раз в сутки загружает агрегированный RU IPv4 list по
HTTPS, проверяет, что сети публичные и их количество правдоподобно, затем
атомарно заменяет nftables set. Текущий production set содержал 8651 prefix.

Пакет к адресу из set получает mark `0x51890`; rule `1088` отправляет его в
`main`, forwarding разрешается только из `wg-users` в `eth0`, а source NAT
выполняется на `utils`.

Это маршрутизация по IP, не по домену. Российский сервис на иностранном CDN
пойдёт через AWG; иностранный сервис с российским edge может пойти напрямую.
Не пытаться «починить» это списком доменов внутри WireGuard.

Если обновление списка упало, остаётся last-known-good set. Если first boot
не смог получить список, set остаётся пустым и весь трафик продолжает идти
через AWG; случайного direct leak нет.

### 6.3 External и fail-closed

Для unmarked source `10.89.0.0/24` rule `1089` использует table `51889`.
Таблица обязана содержать:

```text
default dev <selected awg-exit> metric 10
unreachable default metric 32767
10.89.0.0/24 dev wg-users scope link
```

Connected route обратно к `wg-users` нужен для ответов локального DNS.
`unreachable default` запрещает External-трафику выпадать в main route, когда
exit отсутствует или failover находится в hysteresis.

`systemd-networkd` restart удаляет ephemeral `ip rule`. Поэтому
`my-utils-wireguard-routing-reconcile.service` привязан как `WantedBy` и
запускается после networkd, а agent повторяет тот же idempotent reconcile перед
heartbeat. Stateful routing/geo units нельзя связывать с networkd через
`PartOf=`: их stop action сотрёт выбранный exit или validated RU set.

Relay не маскарадует обычный AWG-трафик: exit видит исходный peer IP
`10.89.0.x`, а final NAT выполняется внутри AWG container. Это необходимо для
per-peer accounting.

### 6.4 Exit failover

Оба AWG interface всегда подняты. Timer раз в 5 секунд отдельно проверяет:

- наличие interface;
- handshake не старше 180 секунд;
- HTTPS через конкретный interface;
- точное совпадение observed и expected public egress;
- опционально ICMP RTT до `1.1.1.1`.

В `AUTO` primary переключается после трёх провалов подряд и возвращается после
двух успешных primary probes. `PRIMARY`/`SECONDARY` из UI задают предпочтение,
но здоровый второй exit остаётся safe fallback. При отказе обоих active default
удаляется, а unreachable route остаётся.

RU-direct и локальный DNS при отказе обоих exit могут продолжать работать;
External обязан fail closed.

### 6.5 API outbound proxy

Production API настроен на proxy selector `91.197.0.191:8888`.
Host chains на `utils`:

1. маркируют только TCP/8888 из Docker CIDR `172.16.0.0/12` к selector mark
   `0x51891`;
2. DNAT destination в `172.29.172.3:8888`;
3. rule `1087` отправляет packet в table `51889`;
4. SNAT делает source `10.89.0.1`, понятный exit stack.

Public IP здесь является селектором, а не реальным network destination. Нельзя
поменять `OPENROUTER_PROXY_HOST` отдельно от `api-proxy-routing.sh`: трафик
перестанет матчиться и уйдёт не туда.

### 6.6 Velocity proxy tunnel

На Velocity persistent config `/etc/wireguard/wg-utils.conf` содержит:

- address `10.89.0.7/32`;
- endpoint `utils.alexeyav.ru:51820`;
- `AllowedIPs = 172.29.172.3/32`;
- keepalive 25 seconds;
- persistent PostUp/PostDown DNAT и SNAT rules.

Селектор ProxyARC `91.197.0.191:8888`; local OUTPUT DNAT меняет
назначение на `172.29.172.3:8888`, а SNAT задаёт source `10.89.0.7`.

Оба exit-хоста намеренно имеют один и тот же Docker subnet и proxy IP. Это
безопасно только потому, что они находятся на разных машинах: выбранный
`table 51889` exit определяет, какой именно `172.29.172.3` будет достигнут.

WireGuard разрешает hostname при старте interface и хранит в kernel уже
разрешённый IP. После изменения A-record надо перезапустить
`wg-quick@wg-utils`; ожидание само по себе endpoint не обновит.

## 7. Exit host design

Каждый exit — отдельный Ubuntu host с isolated Compose project
`my-utils-awg-exit`:

- bridge `amn0`, subnet `172.29.172.0/24`;
- AWG container `172.29.172.2`, limit 128 MiB RAM / 256 MiB memory+swap;
- Tinyproxy `172.29.172.3`, explicit RAM limit 64 MiB; текущий Docker runtime
  даёт effective memory+swap 128 MiB;
- только UDP `42697` опубликован на host;
- AWG config и `.env` mode 600;
- Docker build context ограничен `.dockerignore`;
- swap 2 GiB, bounded journald/Docker logs, fail2ban, key-only SSH.

Tinyproxy принимает соединения только от AWG container IP `172.29.172.2` и
разрешает CONNECT только на `443`. AWG container SNAT-ит client/relay source в
собственный bridge IP перед обращением к proxy.

Container сначала пытается создать kernel `amneziawg` link. Если host kernel
не поддерживает тип, запускается `amneziawg-go`, и installer ждёт socket
`/run/amneziawg/<interface>.sock`. Стандартный WireGuard socket path здесь не
подходит.

Нельзя пересоздавать старые stateful Amnezia containers, найденные вне
`/opt/my-utils-awg-exit`, только ради унификации. Сначала доказать, где лежат
keys/config и есть ли volume. Старый `amnezia-awg` на Veesp хранил всё в
writable container layer; recreate означал бы потерю конфигурации.

## 8. Secrets и backup contract

| Секрет | Где хранить | Потеря |
| --- | --- | --- |
| `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY` | production secret backup | старые tunnels работают, config download/recovery невозможен |
| `wg-users` server private key | encrypted Ansible Vault | после rebuild старые client profiles перестают подключаться |
| Relay agent token | Vault + mode-600 host env | heartbeat/peer convergence получают `401` |
| AWG client private keys | Vault, по одному на exit | соответствующий relay-to-exit tunnel надо перевыпустить |
| Exit AWG server key и PSK | protected generated config/params | конкретный exit надо пересобрать и обновить relay side |
| PostgreSQL backup | обычный encrypted DB backup | теряются peers, revisions, encrypted credentials и history |

Запрещено выводить в chat, CI log или incident paste:

- `wg showconf`, `awg showconf`;
- полные `/etc/my-utils/*.env`;
- `vault.yml` после decrypt;
- `client.params`, private key, PSK;
- полный WireGuard client config.

Для диагностики достаточно `wg show`, `awg show`, whitelist-полей env,
`stat`, status JSON, rules/routes и public egress.

## 9. Развёртывание с нуля через Ansible

### 9.1 Prerequisites

1. Один relay и ровно два exit-хоста на независимых providers.
2. Ubuntu, root SSH по ключу на всех трёх хостах до hardening.
3. На exit нет конфликта с `172.29.172.0/24`, containers с теми же именами и
   unmanaged `/opt/my-utils-awg-exit`.
4. На relay свободны `10.89.0.0/24`, `10.8.1.0/24`, `10.8.2.0/24`, table
   `51889`, priorities `1087..1089`, interfaces `wg-users`, `awg-exit`,
   `awg-exit-b`.
5. DNS `utils.alexeyav.ru` указывает на relay; UDP/51820 разрешён.
6. API и PostgreSQL развёрнуты. Production API на текущем host доступен relay
   agent по `http://127.0.0.1:18080`.
7. Настроен и отдельно сохранён `WIREGUARD_CREDENTIALS_ENCRYPTION_KEY`.
8. Relay record создан, его UUID и одноразовый agent token сохранены.

### 9.2 Inventory и Vault

```bash
cd ops/wireguard/ansible
cp inventory.example.yml inventory.yml
cp vault.example.yml vault.yml
```

В `inventory.yml` заполнить:

- relay address, `vpn_api_base_url`, relay UUID;
- `vpn_public_endpoint: utils.alexeyav.ru:51820`;
- primary/secondary host, role, unique interface и overlay octet;
- AWG UDP endpoint и numeric expected public egress;
- только доверенные administrator source IP для SSH ignore list.

Сгенерировать stable identities с restrictive umask и сразу перенести в
`vault.yml`:

```bash
umask 077
wg genkey
awg genkey
awg genkey
ansible-vault encrypt vault.yml
```

Vault password хранится вне репозитория. Не оставлять plaintext `vault.yml` на
controller и не вставлять вывод keygen в shell history или chat.

### 9.3 Validate, plan, apply

```bash
python3 validate.py \
  --inventory inventory.yml \
  --vault-vars vault.yml

ansible-playbook --ask-vault-pass site.yml
```

Вторая команда выполняет validation/plan и не меняет хосты. После проверки
topology:

```bash
ansible-playbook --ask-vault-pass site.yml -e vpn_apply=true
```

Playbook последовательно:

1. валидирует topology и secret shape;
2. stage-ит только allowlisted `stage-files.txt`;
3. готовит Amnezia tooling и stable AWG client identities на relay;
4. harden-ит exit hosts и ставит isolated AWG/Tinyproxy stacks;
5. передаёт `client.params` только через Ansible memory с `no_log`;
6. поднимает оба relay AWG clients, не выбирая route преждевременно;
7. ставит `wg-users` и agent с временным single-primary routing unit;
8. заменяет временный unit на HA failover и fail-closed table;
9. ставит RU routing и DNS;
10. выполняет post-install assertions.

Установка поверх уже managed state без `vpn_replace` должна отказать. Для
осознанного disaster rebuild:

```bash
ansible-playbook --ask-vault-pass site.yml \
  -e vpn_apply=true \
  -e vpn_replace=true
```

`vpn_replace=true` может ротировать exit server identities и кратко прерывает
interfaces. Это не healthcheck и не способ «переустановить на всякий случай».
Stable `wg-users` key из Vault должен остаться тем же.

## 10. Manual fallback

Ansible только оркестрирует guarded scripts. Если Ansible недоступен, порядок
из [`README.md`](README.md) выполняется вручную, каждый installer сначала без
`--apply`.

Критическая последовательность:

1. `prepare-utils-host.sh` на relay.
2. Для каждого exit создать отдельную AWG client identity на relay.
3. `bootstrap-host.sh` на exit: сначала plan, потом `--apply`.
4. На exit `generate-config.sh`, затем `install.sh` plan/apply.
5. Только protected `client.params` передать на relay.
6. `install-utils-client.sh` для `awg-exit` и `awg-exit-b`; он проверяет
   handshake и egress, не меняя table `51889`.
7. `install-relay.sh` с stable server private key и agent token.
8. `install-awg-failover.sh` plan, затем apply. Он обязан идти после relay,
   потому что заменяет временный single-primary routing unit на HA version;
   `--replace` нужен только для уже managed failover state.
9. `install-geo-routing.sh` и `install-client-dns.sh` plan/apply.
10. Отдельно восстановить Velocity `wg-utils` и выполнить
    `switch-velocity-proxy.sh`.

Не выбирать primary route до того, как оба interface отдельно доказали
`curl --interface <iface> https://api.ipify.org == expected_egress`.

## 11. Verification gate

Зелёные unit’ы не являются достаточным доказательством. Полная проверка после
install/rebuild включает:

```bash
# relay services
systemctl is-active \
  wg-quick@wg-users.service \
  my-utils-awg-exit.service \
  my-utils-awg-exit-b.service \
  my-utils-awg-failover.timer \
  my-utils-wireguard-agent.timer \
  my-utils-geo-routing-update.timer \
  my-utils-wireguard-dns.service \
  my-utils-api-proxy-routing.service

# exact owned rules and fail-closed table
ip rule show
ip route show table 51889

# interfaces without secrets
wg show wg-users
awg show awg-exit
awg show awg-exit-b

# validated state
jq . /var/lib/my-utils-wireguard/exit-health.json
jq . /var/lib/my-utils-wireguard/geo-routing-status.json

# DNS
dig +time=3 +tries=1 +short @10.89.0.1 example.com A
```

Обязательные assertions:

- rules `1087`, `1088`, `1089` существуют и указывают в правильные tables;
- table `51889` содержит selected default, unreachable default и connected
  route к `10.89.0.0/24`;
- оба exit имеют свежий handshake;
- у каждого exit observed egress точно равен expected egress;
- RU prefix count не нулевой после успешного update;
- resolver слушает `10.89.0.1:53`, но не public address;
- Tinyproxy не имеет host PortBindings;
- реальный client видит Internal route напрямую и External через selected
  provider;
- API proxy отвечает через Tinyproxy;
- Velocity имеет fresh `wg-utils` handshake, route к `172.29.172.3`, ping без
  потерь и успешный TCP connect к `172.29.172.3:8888`;
- admin snapshot свежий и Prometheus/Grafana не показывают stale relay.

Проверка с relay не доказывает, что client last mile до UDP/51820 работает.
Хотя бы один реальный device smoke обязателен.

## 12. Controlled failover drill

Только в согласованное окно:

1. Запомнить active exit и убедиться, что оба healthy.
2. Остановить только `my-utils-awg-exit.service` на relay.
3. Первые два failed probes не должны убирать unreachable fallback.
4. На третьем failed probe должен выбраться `awg-exit-b`.
5. Проверить реальный External egress и API proxy.
6. Запустить primary.
7. После двух successful probes `AUTO` должен вернуть primary.
8. Повторно проверить client, DNS, routes и alerts.

Нельзя отключать unreachable route или firewall chains ради теста: такой тест
проверит утечку, а не failover.

## 13. Failure and recovery matrix

| Отказ | Ожидаемое поведение | Восстановление |
| --- | --- | --- |
| API/DB down | текущий data plane работает до reboot; после reboot dynamic peers не загрузятся | восстановить API/DB, дождаться successful desired fetch и heartbeat |
| Agent token mismatch | kernel peers остаются, convergence получает `401` | rotate token и атомарно обновить Vault + host env |
| Encryption key потерян | tunnels работают, config download/reissue недоступен | вернуть backup key или ротировать peers |
| `wg-users` key потерян | старые profiles не подключатся после rebuild | вернуть Vault key или перевыпустить все profiles |
| Один AWG exit down | failover после hysteresis | восстановить host/interface, проверить expected egress |
| Оба exit down | External blocked; RU-direct/DNS могут работать | вернуть хотя бы один exit, не удалять unreachable route |
| RU update failed | остаётся last-known-good; на first boot AWG-only | исправить HTTPS/source/validation и перезапустить updater |
| DNS unit down | client DNS ломается, relay должен стать DEGRADED | восстановить dnsmasq и owned DNS chains |
| `systemd-networkd` restart | ephemeral policy rules могут исчезнуть | reconciler должен восстановить; проверить `1087..1089` |
| Relay public IP changed | running clients держат старый resolved endpoint | обновить DNS, reconnect clients, restart Velocity `wg-utils` |
| Exit public IP changed | соответствующий AWG endpoint/expected egress устаревают | regenerate/install client params, затем controlled selection |
| Tinyproxy accidentally public | security regression | убрать host port и доказать empty PortBindings |
| Docker subnet conflict | installer откажет | выбрать fresh host или осознанно изменить весь address plan |

## 14. Public IP and DNS migration

Relay client profiles используют `utils.alexeyav.ru:51820`, но DNS не
переопределяется внутри уже поднятого WireGuard interface.

Безопасный cutover:

1. заранее снизить TTL;
2. сохранить старый public IP для rollback;
3. добавить новый IP во внешние monitoring allowlists до переключения;
4. обновить все A-records, не только `utils.alexeyav.ru`;
5. reconnect пользовательских tunnels;
6. выполнить на Velocity:

   ```bash
   sudo systemctl restart wg-quick@wg-utils
   ```

7. проверить UDP handshake, web, SSH, Woodpecker webhooks, Prometheus targets,
   API proxy и оба route class;
8. удалить старые allowlists/IP только после DNS cache grace period.

Внешние monitoring allowlists ранее содержали старый relay IP на Velocity и
Gercena. UFW/fail2ban не защищают канал от volumetric SYN/UDP flood; переход на
не защищённый публичный IP является отдельным принятым риском.

## 15. Known production drift on 2026-09-01

- Relay domain всё ещё разрешался в `51.250.112.232`; planned public-IP cutover
  ещё не был виден в live DNS.
- Primary/secondary были healthy, selected primary, preference `AUTO`.
- Primary exit stack работал без `/opt/my-utils-awg-exit/.env`; compose
  использовал default `10.8.1.250/32`. Это допустимый legacy drift, но fresh
  rebuild должен создать mode-600 `.env`. Не пересоздавать healthy primary
  только ради косметического выравнивания.
- ProxyARC и API используют `91.197.0.191:8888` как input для managed
  DNAT. Application config, relay proxy routing и Velocity PostUp/PostDown
  rules должны меняться атомарно.

## 16. Definition of done for another agent

Агент может назвать развёртывание завершённым только если:

1. inventory и encrypted Vault существуют вне Git;
2. plan прошёл без host mutation;
3. apply завершился без bypass guard’ов;
4. secrets не появились в diff/log/chat;
5. оба exit отдельно доказали expected public egress;
6. fail-closed rules/routes прочитаны обратно из kernel;
7. DNS и RU set реально работают;
8. control-plane desired/applied revisions совпали;
9. реальный client smoke подтвердил Internal и External paths;
10. Velocity proxy path и API proxy path проверены отдельно;
11. alerting и свежесть agent heartbeat видны в production;
12. rollback artifacts и backup keys доступны оператору.

Если любой пункт отсутствует, это partial deployment, даже если `systemctl`
показывает `active`.
