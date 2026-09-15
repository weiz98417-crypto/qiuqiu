export function qiuqiuBaseURL(env = process.env) {
  return (env.QIUQIU_BASE_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');
}
