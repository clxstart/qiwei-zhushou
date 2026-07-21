import http from 'http';
import fs from 'fs';
import { execSync } from 'child_process';

function req(method, path, { headers = {}, body = null, cookie = '' } = {}) {
  return new Promise((resolve, reject) => {
    const data = body == null ? null : Buffer.isBuffer(body) ? body : Buffer.from(body);
    const h = { ...headers };
    if (cookie) h.Cookie = cookie;
    if (data) h['Content-Length'] = data.length;
    const r = http.request(
      { hostname: '127.0.0.1', port: 8080, path, method, headers: h },
      (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => {
          const buf = Buffer.concat(chunks);
          let json = null;
          try {
            json = JSON.parse(buf.toString());
          } catch {}
          resolve({
            status: res.statusCode,
            body: buf.toString(),
            json,
            cookies: res.headers['set-cookie'] || [],
          });
        });
      },
    );
    r.on('error', reject);
    if (data) r.write(data);
    r.end();
  });
}

const jar = (set) => set.map((x) => x.split(';')[0]).join('; ');
const results = [];
const check = (name, ok, extra = '') => {
  results.push({ name, ok: !!ok, extra: String(extra).slice(0, 240) });
  if (!ok) throw new Error(`${name}: ${extra}`);
};

const chatId = `e2e_chat_${Math.floor(Date.now() / 1000)}`;
const now = Math.floor(Date.now() / 1000);

// seed group via mysql (single-line SQL for Windows mysql -e)
const members = JSON.stringify([
  { userid: 'e2e_creator', name: 'creator', type: 1 },
  { userid: 'e2e_tech', name: 'tech', type: 1 },
  { userid: 'boss', name: 'boss', type: 2 },
]).replaceAll("'", "''");
const steps = JSON.stringify([
  { name: 'role_announcement', status: 'done' },
  { name: 'delivery_notice', status: 'done' },
  { name: 'biz_flow_text', status: 'done' },
  { name: 'biz_feature_text', status: 'done' },
  { name: 'biz_extend_text', status: 'done' },
  { name: 'rename_group', status: 'done' },
  { name: 'tech_intro', status: 'done' },
  { name: 'account_card', status: 'done' },
]).replaceAll("'", "''");
const sql =
  `INSERT INTO group_chats (chat_id,name,creator_userid,is_external,members_json,selected_tech_userid,selected_tech_name,selected_tech_tag,steps_json,created_at,updated_at) VALUES ` +
  `('${chatId}','E2E-Group','e2e_creator',1,'${members}','e2e_tech','E2E','E2E','${steps}',${now},${now});`;
fs.writeFileSync('scripts/_e2e-seed.sql', sql);
const mysqlUser = process.env.MYSQL_USER || 'qw';
const mysqlPass = process.env.MYSQL_PASSWORD || 'qw_pass';
const mysqlDB = process.env.MYSQL_DB || 'qiwei_zhushou';
execSync(
  `mysql -u${mysqlUser} -p${mysqlPass} --default-character-set=utf8mb4 ${mysqlDB} < scripts/_e2e-seed.sql`,
  {
    stdio: ['ignore', 'pipe', 'pipe'],
    shell: true,
  },
);

(async () => {
  let r = await req('GET', '/api/v1/health');
  check('health', r.status === 200 && r.json?.ok, r.body);

  r = await req('POST', '/api/v1/admin/login', {
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'admin', password: 'admin123' }),
  });
  check('admin login', r.status === 200, r.body);
  const cookie = jar(r.cookies);

  r = await req('POST', `/api/v1/staff/groups/${chatId}/payment`, {
    headers: { 'Content-Type': 'application/json', 'X-WeCom-UserID': 'e2e_creator' },
    body: JSON.stringify({ deposit: 3000, total: 8000 }),
  });
  check('staff payment', r.status === 200 && r.json?.tail === 5000 && r.json?.status === 'booked', r.body);

  r = await req('POST', `/api/v1/staff/groups/${chatId}/payment`, {
    headers: { 'Content-Type': 'application/json', 'X-WeCom-UserID': 'stranger' },
    body: JSON.stringify({ deposit: 1, total: 2 }),
  });
  check('forbidden non-creator', r.status === 403, r.body);

  r = await req('POST', `/api/v1/staff/groups/${chatId}/send-code`, {
    headers: { 'X-WeCom-UserID': 'e2e_creator' },
  });
  check('send code', r.status === 200 && r.json?.code_sent_count === 1 && r.json?.status === 'code_sent', r.body);

  r = await req('POST', `/api/v1/staff/groups/${chatId}/settle`, {
    headers: { 'X-WeCom-UserID': 'e2e_creator' },
  });
  check('settle', r.status === 200 && r.json?.status === 'settled', r.body);

  r = await req('POST', `/api/v1/staff/groups/${chatId}/settle`, {
    headers: { 'X-WeCom-UserID': 'e2e_creator' },
  });
  check('re-settle conflict', r.status === 409, r.body);

  r = await req('POST', `/api/v1/admin/groups/${chatId}/reverse-settle`, { cookie });
  check('reverse settle', r.status === 200 && r.json?.status === 'code_sent', r.body);

  r = await req('POST', `/api/v1/admin/groups/${chatId}/settle`, { cookie });
  check('admin settle', r.status === 200 && r.json?.status === 'settled', r.body);

  r = await req('GET', '/api/v1/admin/summary', { cookie });
  check('summary', r.status === 200 && r.json?.group_count >= 1 && r.json?.settled_count >= 1, r.body);

  r = await req('GET', '/api/v1/admin/groups', { cookie });
  check(
    'groups list',
    r.status === 200 && (r.json?.items || []).some((i) => i.chat_id === chatId && i.settled),
    r.body,
  );

  r = await req('GET', `/api/v1/admin/groups/${chatId}/detail`, { cookie });
  check('group detail', r.status === 200 && r.json?.ledger && r.json?.group, r.body);

  r = await req('GET', `/api/v1/admin/groups/${chatId}/op-logs`, { cookie });
  check('op logs', r.status === 200 && (r.json?.items || []).length >= 1, r.body);

  r = await req('GET', '/api/v1/admin/summary');
  check('admin auth required', r.status === 401, r.body);

  r = await req('GET', `/h5/payment.html?chat_id=${chatId}`);
  check('h5 payment page', r.status === 200 && r.body.includes('录入'), r.status);

  console.log(JSON.stringify({ chatId, results, allPassed: results.every((x) => x.ok) }, null, 2));
})().catch((e) => {
  console.error(e);
  console.log(JSON.stringify({ chatId, results }, null, 2));
  process.exit(1);
});
