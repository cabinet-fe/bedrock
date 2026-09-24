import { bareHttp, clearTokens, http, setAccessToken } from "./http";
import type { LoginResponse, MeResponse } from "./types";

export async function loginApi(username: string, passwordCipher: string): Promise<LoginResponse> {
  const { body } = await bareHttp.post<LoginResponse>("/auth/login", {
    username,
    password_cipher: passwordCipher,
  });
  return body;
}

export async function registerApi(
  username: string,
  passwordCipher: string,
  roleCode: string,
): Promise<LoginResponse> {
  const { body } = await bareHttp.post<LoginResponse>("/auth/register", {
    username,
    password_cipher: passwordCipher,
    role_code: roleCode,
  });
  return body;
}

export async function logoutApi(): Promise<void> {
  await http.post("/auth/logout");
}

export async function meApi(): Promise<MeResponse> {
  const { body } = await http.get<MeResponse>("/auth/me");
  return body;
}

export { clearTokens, setAccessToken };
export type { LoginResponse, MeResponse };
