import { ensureAccessToken } from "@/api/http";

// <a> 不能带 Authorization。下载接口在 GET 上接受 ?token=，交给浏览器原生下载。
export async function triggerNativeDownload(path: string): Promise<void> {
  const token = await ensureAccessToken();
  if (!token) {
    throw new Error("登录已过期");
  }
  const url = new URL(path, location.origin);
  url.searchParams.set("token", token);
  const a = document.createElement("a");
  a.href = `${url.pathname}${url.search}`;
  document.body.appendChild(a);
  a.click();
  a.remove();
}
