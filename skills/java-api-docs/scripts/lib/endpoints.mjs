import fs from 'fs';
import path from 'path';
import { walkJavaFiles, readText } from './fs-utils.mjs';
import {
  stripComments,
  lineAt,
  findAnnotationsBefore,
  firstStringLiteral,
  parseRequestMethods,
  joinPaths,
  mapJavaType,
  mappingPathFromArgs,
  simpleTypeName,
  matchBalanced,
} from './java-parse.mjs';
import { controllerToKebab, controllerDocFileName } from './names.mjs';
import {
  collectServiceNamesDetailed,
  joinGatewayPath,
  resolveGatewayPrefix,
  resolveGatewayJsonPath,
} from './gateway.mjs';

const MAPPING_NAMES = new Set([
  'GetMapping',
  'PostMapping',
  'PutMapping',
  'DeleteMapping',
  'PatchMapping',
  'RequestMapping',
]);

const METHOD_BY_ANNO = {
  GetMapping: ['GET'],
  PostMapping: ['POST'],
  PutMapping: ['PUT'],
  DeleteMapping: ['DELETE'],
  PatchMapping: ['PATCH'],
};

const SKIP_PARAM_TYPES = new Set([
  'HttpServletRequest',
  'HttpServletResponse',
  'ServletRequest',
  'ServletResponse',
  'Principal',
  'Authentication',
  'BindingResult',
  'Model',
  'ModelMap',
  'Errors',
  'SessionStatus',
  'UriComponentsBuilder',
]);

function extractAuth(annos) {
  const hits = [];
  for (const a of annos) {
    if (a.name === 'PreAuthorize' || a.name === 'PostAuthorize') {
      hits.push({ kind: a.name, value: firstStringLiteral(a.args) || a.args.trim() });
    } else if (a.name === 'HasPermission' || a.name === 'Secured' || a.name === 'RolesAllowed') {
      hits.push({ kind: a.name, value: firstStringLiteral(a.args) || a.args.trim() });
    } else if (a.name === 'SecurityRequirement') {
      const nameLit = firstStringLiteral(a.args);
      const nameArg = (nameLit || a.args.match(/\bname\s*=\s*([^,)]+)/)?.[1] || '').trim();
      hits.push({ kind: a.name, value: nameArg });
    } else if (a.name === 'AnonymousAccess') {
      hits.push({ kind: a.name, value: '' });
    }
  }
  return hits;
}

/**
 * 把 SpEL / 权限注解收成中文摘要，供写文档用（不要把原始表达式贴进 Markdown）。
 * 例: #commonDTO.moduleCode+':create' → 需要登录；权限 {moduleCode}:create
 */
function summarizeAuth(hits) {
  if (!hits || !hits.length) return '无需登录（本接口无鉴权注解）';
  if (hits.some((h) => h.kind === 'AnonymousAccess')) return '无需登录（@AnonymousAccess）';
  const parts = [];
  for (const h of hits) {
    if (h.kind === 'SecurityRequirement') {
      const v = (h.value || '').trim();
      if (!v || /AUTHORIZATION/i.test(v)) {
        parts.push('需要登录');
      } else {
        parts.push('需要登录（见 SecurityRequirement）');
      }
      continue;
    }
    const v = (h.value || '').trim();
    if (!v) {
      parts.push('需要登录');
      continue;
    }
    // #foo.moduleCode+':create' / #moduleCode+":view"
    const dyn = v.match(/#([\w.]+)\s*\+\s*['"](:[^'"]+)['"]/);
    if (dyn) {
      const ref = dyn[1];
      const action = dyn[2];
      const moduleHint = /\.?moduleCode$/.test(ref) ? '{moduleCode}' : `{${ref}}`;
      parts.push(`需要登录；权限 ${moduleHint}${action}`);
      continue;
    }
    // hasPermission('a:b') / hasAuthority('ROLE_X')
    const litPerm = v.match(/has(?:Permission|Authority)\s*\(\s*['"]([^'"]+)['"]\s*\)/i);
    if (litPerm) {
      parts.push(`需要登录；权限 ${litPerm[1]}`);
      continue;
    }
    // hasRole('ADMIN') / hasAnyRole('A','B')
    const role = v.match(/has(?:Any)?Role\s*\(\s*['"]([^'"]+)['"]/);
    if (role) {
      parts.push(`需要登录；角色 ${role[1]}`);
      continue;
    }
    // @HasPermission("x:y") 等直接字符串
    if (!/[()#]/.test(v) && v.length < 80) {
      parts.push(`需要登录；权限 ${v}`);
      continue;
    }
    parts.push('需要登录（见源码鉴权注解；请写成权限码摘要，勿贴 SpEL 原文）');
  }
  // 去重保序；多条权限用顿号连接
  const unique = [...new Set(parts)];
  if (unique.length === 1) return unique[0];
  const perms = unique
    .map((p) => {
      const m = p.match(/^需要登录；权限\s+(.+)$/);
      return m ? m[1] : null;
    })
    .filter(Boolean);
  if (perms.length === unique.length) {
    return `需要登录；权限 ${perms.join('、')}`;
  }
  return unique.join('；');
}

function parseMethodParams(paramSrc) {
  const params = [];
  if (!paramSrc || !paramSrc.trim()) return params;
  // 按逗号拆分，忽略 <> / () 内的逗号
  const parts = [];
  let cur = '';
  let angle = 0;
  let paren = 0;
  for (let i = 0; i < paramSrc.length; i += 1) {
    const c = paramSrc[i];
    if (c === '<') angle += 1;
    else if (c === '>') angle -= 1;
    else if (c === '(') paren += 1;
    else if (c === ')') paren -= 1;
    if (c === ',' && angle === 0 && paren === 0) {
      parts.push(cur);
      cur = '';
      continue;
    }
    cur += c;
  }
  if (cur.trim()) parts.push(cur);

  for (const part of parts) {
    const p = part.trim();
    if (!p) continue;
    const annos = [];
    const re = /@([A-Za-z_][A-Za-z0-9_]*)(\s*\((?:[^()]|\([^()]*\))*\))?/g;
    let m;
    let lastAnnoEnd = 0;
    while ((m = re.exec(p))) {
      annos.push({
        name: m[1],
        args: m[2] ? m[2].replace(/^\s*\(/, '').replace(/\)$/, '') : '',
      });
      lastAnnoEnd = m.index + m[0].length;
    }
    const rest = p.slice(lastAnnoEnd).trim();
    const tokens = rest.split(/\s+/);
    if (tokens.length < 1) continue;
    const name = tokens[tokens.length - 1].replace(/,$/, '');
    const typeExpr = tokens.slice(0, -1).join(' ') || tokens[0];
    if (SKIP_PARAM_TYPES.has(simpleTypeName(typeExpr))) continue;

    const pathVar = annos.find((a) => a.name === 'PathVariable');
    const reqParam = annos.find((a) => a.name === 'RequestParam');
    const reqBody = annos.find((a) => a.name === 'RequestBody');
    const mapped = mapJavaType(typeExpr);

    if (pathVar) {
      const n = firstStringLiteral(pathVar.args) || name;
      params.push({
        in: 'path',
        name: n,
        required: !/\brequired\s*=\s*false\b/.test(pathVar.args || ''),
        ...mapped,
      });
    } else if (reqParam) {
      const n = firstStringLiteral(reqParam.args) || name;
      const required = !/\brequired\s*=\s*false\b/.test(reqParam.args || '');
      params.push({ in: 'query', name: n, required, ...mapped });
    } else if (reqBody) {
      params.push({ in: 'body', name, required: true, ...mapped });
    }
  }
  return params;
}

function findClassDeclaration(src) {
  const re = /\b((?:public|protected|private)\s+)?((?:abstract|final)\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)/g;
  let m;
  while ((m = re.exec(src))) {
    const annos = findAnnotationsBefore(src, m.index);
    const isRest = annos.some((a) => a.name === 'RestController' || a.name === 'Controller');
    if (!isRest) continue;
    const className = m[3];
    const classAnnos = annos;
    const brace = src.indexOf('{', m.index);
    return { className, classAnnos, bodyStart: brace >= 0 ? brace : m.index, index: m.index };
  }
  return null;
}

function classRequestMapping(classAnnos) {
  const rm = classAnnos.filter((a) => a.name === 'RequestMapping').pop();
  if (!rm) return '';
  return mappingPathFromArgs(rm.args);
}

/** methodRe 可能把 @PostMapping 等无参注解名误吞进「返回类型」；此类匹配须跳过并重扫。 */
function isFalseMethodMatch(src, m) {
  if (m.index > 0 && src[m.index - 1] === '@') return true;
  if (MAPPING_NAMES.has(m[3])) return true;
  if (/\b(?:Get|Post|Put|Delete|Patch|Request)Mapping\b/.test(m[2] || '')) return true;
  return false;
}

function parseControllerFile(filePath, srcRaw) {
  const src = stripComments(srcRaw);
  const cls = findClassDeclaration(src);
  if (!cls) return [];

  const basePath = classRequestMapping(cls.classAnnos);
  const classAuth = extractAuth(cls.classAnnos);
  const endpoints = [];

  // 带前置映射注解的方法声明
  const methodRe =
    /\b((?:public|protected|private)\s+)?([\w.<>,\s\[\]?]+?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(/g;
  let m;
  while ((m = methodRe.exec(src))) {
    if (m.index < cls.bodyStart) continue;
    if (isFalseMethodMatch(src, m)) {
      methodRe.lastIndex = m.index + 1;
      continue;
    }
    const methodName = m[3];
    if (methodName === cls.className) continue; // 构造函数
    const returnType = m[2].trim();
    if (/\b(if|while|for|switch|catch|return|new)\b/.test(returnType)) continue;

    const annos = findAnnotationsBefore(src, m.index);
    const mapping = annos.filter((a) => MAPPING_NAMES.has(a.name)).pop();
    if (!mapping) continue;

    let methods = METHOD_BY_ANNO[mapping.name];
    if (!methods) {
      methods = parseRequestMethods(mapping.args);
      if (!methods.length) methods = ['REQUEST'];
    }
    const subPath = mappingPathFromArgs(mapping.args);
    const fullPath = joinPaths(basePath, subPath);

    const paren = matchBalanced(src, m.index + m[0].length - 1);
    const paramSrc = paren ? paren.inner : '';
    const params = parseMethodParams(paramSrc);
    const auth = extractAuth(annos);
    const effectiveAuth = auth.length ? auth : classAuth;

    const returnMapped = mapJavaType(returnType);
    const line = lineAt(srcRaw, m.index);

    for (const httpMethod of methods) {
      endpoints.push({
        controller: cls.className,
        handler: methodName,
        method: httpMethod,
        /** 服务内 path（Controller 映射）；对外完整 path 见 listEndpoints 注入的 path */
        servicePath: fullPath,
        path: fullPath,
        auth: effectiveAuth,
        authSummary: summarizeAuth(effectiveAuth),
        requiresAuth: effectiveAuth.length > 0,
        params,
        pathParams: params.filter((p) => p.in === 'path'),
        queryParams: params.filter((p) => p.in === 'query'),
        body: params.find((p) => p.in === 'body') || null,
        returnType: returnMapped,
        file: filePath,
        line,
      });
    }
  }
  return endpoints;
}

/**
 * @param {string} srcRoot
 * @param {{
 *   files?: string[]|null,
 *   repoRoot?: string|null,
 *   project?: string|null,
 *   service?: string|null,
 *   gatewayJson?: string|null,
 *   gateway?: string|null,
 * }} [opts]
 */
function listEndpoints(srcRoot, opts = {}) {
  const { files } = opts;
  const root = path.resolve(srcRoot);
  let javaFiles;
  if (files && files.length) {
    javaFiles = files.map((f) => {
      if (path.isAbsolute(f)) return f;
      const underRoot = path.resolve(root, f);
      if (fs.existsSync(underRoot)) return underRoot;
      // 相对 cwd 的路径（Agent 常传仓库相对路径）
      return path.resolve(process.cwd(), f);
    });
  } else {
    javaFiles = walkJavaFiles(root, {
      filter: (f) => /Controller\.java$/.test(f) || /controller\//i.test(f),
    });
    // 同时包含未以 *Controller 命名的 RestController
    const extras = walkJavaFiles(root, {
      filter: (f) => f.endsWith('.java') && !javaFiles.includes(f),
    });
    for (const f of extras) {
      const text = readText(f);
      if (/@RestController\b/.test(text) || /@Controller\b/.test(text)) javaFiles.push(f);
    }
    javaFiles.sort();
  }

  const collected = collectServiceNamesDetailed({
    repoRoot: opts.repoRoot || root,
    srcRoot: root,
    service: opts.service,
    project: opts.project,
  });
  const serviceNames = collected.names;
  const gateway = resolveGatewayPrefix({
    serviceNames,
    gatewayJson: opts.gatewayJson ?? opts.gateway,
  });
  // 多模块歧义警告挂到 gateway（matched 仍可能因 --project 命中而为 true）
  if (collected.warnings.length) {
    gateway.serviceWarnings = collected.warnings;
    const extra = collected.warnings.join('；');
    gateway.warning = gateway.warning ? `${gateway.warning}；${extra}` : extra;
  }

  const endpoints = [];
  const errors = [];
  /** @type {Map<string, { controller: string, docFile: string, file: string, endpoints: any[] }>} */
  const byController = new Map();

  for (const f of javaFiles) {
    try {
      const raw = readText(f);
      if (!/@RestController\b/.test(raw) && !/@Controller\b/.test(raw)) continue;
      const eps = parseControllerFile(f, raw);
      const relFile = path.relative(root, f) || f;
      for (const ep of eps) {
        ep.file = relFile;
        ep.servicePath = ep.servicePath || ep.path;
        ep.path = joinGatewayPath(gateway.gatewayPrefix, ep.servicePath);
        ep.gatewayPrefix = gateway.gatewayPrefix;
        ep.gatewayMatched = gateway.matched;
        if (!gateway.matched) {
          ep.gatewayWarning = gateway.warning;
        }
        ep.docFile = controllerDocFileName(ep.controller);
        ep.docSlug = controllerToKebab(ep.controller);
        endpoints.push(ep);

        const key = ep.controller;
        if (!byController.has(key)) {
          byController.set(key, {
            controller: ep.controller,
            docFile: ep.docFile,
            docSlug: ep.docSlug,
            file: relFile,
            endpoints: [],
          });
        }
        byController.get(key).endpoints.push(ep);
      }
    } catch (err) {
      errors.push({ file: f, error: err.message });
    }
  }

  const controllers = [...byController.values()].sort((a, b) =>
    a.docFile.localeCompare(b.docFile),
  );

  return {
    srcRoot: root,
    count: endpoints.length,
    endpoints,
    controllers,
    errors,
    gateway: {
      matched: gateway.matched,
      gatewayPrefix: gateway.gatewayPrefix,
      serviceName: gateway.serviceName,
      triedNames: gateway.triedNames,
      gatewayJson:
        path.relative(process.cwd(), resolveGatewayJsonPath(opts.gatewayJson ?? opts.gateway)) ||
        resolveGatewayJsonPath(opts.gatewayJson ?? opts.gateway),
      warning: gateway.warning,
      defaultStripPrefix: gateway.defaultStripPrefix,
      routeId: gateway.route ? gateway.route.id : null,
    },
  };
}

function isRelevantJavaChange(relPath) {
  if (!relPath.endsWith('.java')) return false;
  const base = path.basename(relPath);
  if (base.endsWith('Controller.java')) return true;
  if (/\/(dto|vo|entity|domain|model)\//i.test(relPath)) return true;
  if (/DTO\.java$|VO\.java$|Request\.java$|Response\.java$|Entity\.java$/.test(base)) return true;
  return /controller\//i.test(relPath);
}

export {
  listEndpoints,
  parseControllerFile,
  isRelevantJavaChange,
  summarizeAuth,
  controllerToKebab,
  controllerDocFileName,
};
