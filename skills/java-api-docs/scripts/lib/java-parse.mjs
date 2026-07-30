function stripComments(src) {
  let out = '';
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];
    const n1 = src[i + 1];
    if (c === '/' && n1 === '/') {
      i += 2;
      while (i < n && src[i] !== '\n') i += 1;
      continue;
    }
    if (c === '/' && n1 === '*') {
      i += 2;
      while (i + 1 < n && !(src[i] === '*' && src[i + 1] === '/')) i += 1;
      i += 2;
      out += ' ';
      continue;
    }
    if (c === '"' || c === "'") {
      const quote = c;
      out += c;
      i += 1;
      while (i < n) {
        out += src[i];
        if (src[i] === '\\') {
          i += 1;
          if (i < n) {
            out += src[i];
            i += 1;
          }
          continue;
        }
        if (src[i] === quote) {
          i += 1;
          break;
        }
        i += 1;
      }
      continue;
    }
    out += c;
    i += 1;
  }
  return out;
}

function lineAt(src, index) {
  let line = 1;
  for (let i = 0; i < index && i < src.length; i += 1) {
    if (src[i] === '\n') line += 1;
  }
  return line;
}

function matchBalanced(src, openIdx, openCh = '(', closeCh = ')') {
  if (src[openIdx] !== openCh) return null;
  let depth = 0;
  for (let i = openIdx; i < src.length; i += 1) {
    const c = src[i];
    if (c === '"' || c === "'") {
      const q = c;
      i += 1;
      while (i < src.length) {
        if (src[i] === '\\') {
          i += 2;
          continue;
        }
        if (src[i] === q) break;
        i += 1;
      }
      continue;
    }
    if (c === openCh) depth += 1;
    else if (c === closeCh) {
      depth -= 1;
      if (depth === 0) return { start: openIdx, end: i, inner: src.slice(openIdx + 1, i) };
    }
  }
  return null;
}

function extractAnnotationArgs(src, atIndex) {
  // atIndex 指向 '@'
  let i = atIndex + 1;
  while (i < src.length && /[A-Za-z0-9_.]/.test(src[i])) i += 1;
  const name = src.slice(atIndex + 1, i);
  const nameEnd = i;
  while (i < src.length && /\s/.test(src[i])) i += 1;
  if (src[i] !== '(') {
    // 无参注解 end 止于注解名；若吞掉其后空白，下一行 public 会使 end > methodIndex
    return { name, args: '', end: nameEnd };
  }
  const bal = matchBalanced(src, i);
  if (!bal) return { name, args: '', end: i };
  return { name, args: bal.inner, end: bal.end + 1 };
}

function isOnlyWhitespaceAndComments(text) {
  let i = 0;
  const n = text.length;
  while (i < n) {
    if (/\s/.test(text[i])) {
      i += 1;
      continue;
    }
    if (text[i] === '/' && text[i + 1] === '/') {
      i += 2;
      while (i < n && text[i] !== '\n') i += 1;
      continue;
    }
    if (text[i] === '/' && text[i + 1] === '*') {
      i += 2;
      while (i + 1 < n && !(text[i] === '*' && text[i + 1] === '/')) i += 1;
      i += 2;
      continue;
    }
    return false;
  }
  return true;
}

function isInsideStringLiteral(src, absIndex) {
  // 必须从文件开头扫，不能从 lookback 窗口中部起扫（否则会切在注解字符串中间、引号状态全反）
  // 注解参数几乎都是双引号；SpEL 内 ':action' 单引号不当字符串界
  let inStr = false;
  for (let i = 0; i < absIndex; i += 1) {
    const c = src[i];
    if (inStr) {
      if (c === '\\') {
        i += 1;
        continue;
      }
      if (c === '"') inStr = false;
      continue;
    }
    if (c === '"') inStr = true;
  }
  return inStr;
}

function findAnnotationsBefore(src, index, lookback = 800) {
  const start = Math.max(0, index - lookback);
  const slice = src.slice(start, index);
  const annos = [];
  const re = /@([A-Za-z_][A-Za-z0-9_.]*)/g;
  let m;
  while ((m = re.exec(slice))) {
    const abs = start + m.index;
    if (isInsideStringLiteral(src, abs)) continue;
    const { name, args, end } = extractAnnotationArgs(src, abs);
    if (end > index) continue;
    annos.push({
      name: name.includes('.') ? name.split('.').pop() : name,
      fullName: name,
      args,
      index: abs,
      end,
    });
  }
  if (!annos.length) return [];

  // 只保留紧贴声明的连续注解块，避免 lookback 扫到上一方法的 @PreAuthorize
  const trailing = [];
  let cursor = index;
  for (let k = annos.length - 1; k >= 0; k -= 1) {
    const a = annos[k];
    if (!isOnlyWhitespaceAndComments(src.slice(a.end, cursor))) break;
    trailing.unshift(a);
    cursor = a.index;
  }
  return trailing;
}

function firstStringLiteral(args) {
  if (!args) return '';
  const m = args.match(/"((?:\\.|[^"\\])*)"/);
  return m ? m[1].replace(/\\"/g, '"') : '';
}

function allStringLiterals(args) {
  if (!args) return [];
  const out = [];
  const re = /"((?:\\.|[^"\\])*)"/g;
  let m;
  while ((m = re.exec(args))) out.push(m[1].replace(/\\"/g, '"'));
  return out;
}

function parseRequestMethods(args) {
  if (!args) return [];
  const methods = [];
  const re = /RequestMethod\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)/g;
  let m;
  while ((m = re.exec(args))) methods.push(m[1]);
  return methods;
}

function joinPaths(base, sub) {
  const a = (base || '').trim();
  const b = (sub || '').trim();
  if (!a && !b) return '/';
  const parts = [];
  for (const p of [a, b]) {
    if (!p) continue;
    parts.push(p.replace(/^\/+|\/+$/g, ''));
  }
  const joined = `/${parts.filter(Boolean).join('/')}`.replace(/\/+/g, '/');
  return joined === '' ? '/' : joined;
}

function simpleTypeName(typeExpr) {
  if (!typeExpr) return '';
  let t = typeExpr.trim();
  // 去掉类型位置上的注解
  t = t.replace(/@\w+(?:\([^)]*\))?\s+/g, '');
  t = t.replace(/\b(?:final|volatile|transient)\b\s+/g, '');
  // 取泛型前最后一段作为简单名
  const noGen = t.replace(/<.*>/, '').trim();
  const parts = noGen.split(/\s+/);
  const last = parts[parts.length - 1] || noGen;
  const simple = last.includes('.') ? last.split('.').pop() : last;
  return simple;
}

function mapJavaType(typeExpr) {
  if (!typeExpr) return { type: 'object', javaType: typeExpr || '' };
  const raw = typeExpr
    .trim()
    .replace(/@\w+(?:\([^)]*\))?\s+/g, '')
    .replace(/\b(?:public|protected|private|static|final|volatile|transient)\b\s+/g, '')
    .trim();
  const javaType = raw;

  const listMatch = raw.match(/^(?:java\.util\.)?(?:List|Set|Collection|Iterable)\s*<\s*([\s\S]+)\s*>$/);
  if (listMatch) {
    const inner = mapJavaType(listMatch[1].trim());
    return { type: `${inner.type}[]`, javaType, item: inner };
  }
  if (/\[\]\s*$/.test(raw)) {
    const inner = mapJavaType(raw.replace(/\[\]\s*$/, '').trim());
    return { type: `${inner.type}[]`, javaType, item: inner };
  }
  const mapMatch = raw.match(/^(?:java\.util\.)?Map\s*<\s*([\s\S]+)\s*,\s*([\s\S]+)\s*>$/);
  if (mapMatch) {
    const val = mapJavaType(mapMatch[2].trim());
    return { type: `Record<string, ${val.type}>`, javaType, value: val };
  }
  const pageMatch = raw.match(/^(?:[\w.]+)?Page\s*<\s*([\s\S]+)\s*>$/);
  if (pageMatch) {
    const inner = mapJavaType(pageMatch[1].trim());
    return { type: 'object', javaType, pageOf: inner, note: 'Page 分页包装' };
  }
  const rMatch = raw.match(/^(?:[\w.]+)?R\s*<\s*([\s\S]+)\s*>$/);
  if (rMatch) {
    const inner = mapJavaType(rMatch[1].trim());
    return { type: 'object', javaType, envelopeData: inner, note: 'R 响应信封' };
  }

  const simple = simpleTypeName(raw);
  const primitives = {
    String: 'string',
    CharSequence: 'string',
    char: 'string',
    Character: 'string',
    boolean: 'boolean',
    Boolean: 'boolean',
    byte: 'number',
    Byte: 'number',
    short: 'number',
    Short: 'number',
    int: 'number',
    Integer: 'number',
    long: 'number',
    Long: 'number',
    float: 'number',
    Float: 'number',
    double: 'number',
    Double: 'number',
    BigDecimal: 'number',
    BigInteger: 'number',
    Number: 'number',
    void: 'null',
    Void: 'null',
    Object: 'object',
  };
  if (primitives[simple]) return { type: primitives[simple], javaType, simple };
  return { type: 'object', javaType, simple, needs_source: true };
}

function parseNamedArgs(args) {
  // 轻量解析：path="/x", value="/x", required=false
  const result = {};
  if (!args) return result;
  const path = firstStringLiteral(args);
  if (path) result.path = path;
  if (/\brequired\s*=\s*false\b/.test(args)) result.required = false;
  else if (/\brequired\s*=\s*true\b/.test(args)) result.required = true;
  const nameMatch = args.match(/\bname\s*=\s*"((?:\\.|[^"\\])*)"/);
  if (nameMatch) result.name = nameMatch[1];
  const valueMatch = args.match(/\bvalue\s*=\s*"((?:\\.|[^"\\])*)"/);
  if (valueMatch) result.value = valueMatch[1];
  return result;
}

function mappingPathFromArgs(args) {
  const named = parseNamedArgs(args);
  if (named.path) return named.path;
  if (named.value) return named.value;
  const strings = allStringLiterals(args);
  return strings[0] || '';
}

export {
  stripComments,
  lineAt,
  matchBalanced,
  extractAnnotationArgs,
  findAnnotationsBefore,
  firstStringLiteral,
  allStringLiterals,
  parseRequestMethods,
  joinPaths,
  simpleTypeName,
  mapJavaType,
  parseNamedArgs,
  mappingPathFromArgs,
};
