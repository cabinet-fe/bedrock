import path from 'path';
import { walkJavaFiles, readText } from './fs-utils.mjs';
import {
  stripComments,
  mapJavaType,
  simpleTypeName,
  findAnnotationsBefore,
} from './java-parse.mjs';

function buildTypeIndex(srcRoot) {
  const root = path.resolve(srcRoot);
  const files = walkJavaFiles(root);
  const index = new Map(); // 简单类名 -> [{ file, packageName, fqcn, raw }]
  for (const f of files) {
    const raw = readText(f);
    const pkgMatch = raw.match(/^\s*package\s+([\w.]+)\s*;/m);
    const packageName = pkgMatch ? pkgMatch[1] : '';
    const classRe =
      /\b((?:public|protected|private)\s+)?((?:static\s+)?(?:abstract\s+|final\s+)?)(class|interface|enum|record)\s+([A-Za-z_][A-Za-z0-9_]*)/g;
    let m;
    const src = stripComments(raw);
    while ((m = classRe.exec(src))) {
      const name = m[4];
      const fqcn = packageName ? `${packageName}.${name}` : name;
      const entry = { file: f, relative: path.relative(root, f), packageName, fqcn, kind: m[3], raw, src };
      if (!index.has(name)) index.set(name, []);
      index.get(name).push(entry);
    }
  }
  return { root, index };
}

function pickType(index, typeName) {
  const simple = simpleTypeName(typeName) || typeName;
  const list = index.get(simple);
  if (!list || !list.length) return null;
  if (typeName.includes('.')) {
    const hit = list.find((e) => e.fqcn === typeName || e.fqcn.endsWith(`.${simple}`));
    return hit || list[0];
  }
  return list[0];
}

function parseFieldsFromClass(entry) {
  const { src } = entry;
  const classRe = new RegExp(
    `\\b(?:public|protected|private)?\\s*(?:static\\s+)?(?:abstract\\s+|final\\s+)?(?:class|record)\\s+${entry.fqcn.split('.').pop()}\\b`,
  );
  const cm = classRe.exec(src);
  if (!cm) return { fields: [], extendsType: null, lombok: false };

  const annos = findAnnotationsBefore(src, cm.index);
  const lombok = annos.some((a) =>
    ['Data', 'Value', 'Getter', 'Setter', 'Builder', 'SuperBuilder'].includes(a.name),
  );

  let extendsType = null;
  const after = src.slice(cm.index, cm.index + 400);
  const ext = after.match(/\bextends\s+([A-Za-z_][\w.<>,\s]*)/);
  if (ext) {
    extendsType = ext[1].trim().split(/\s+implements/)[0].trim();
  }

  const braceStart = src.indexOf('{', cm.index);
  if (braceStart < 0) return { fields: [], extendsType, lombok };

  // 类体：在深度 1 收集字段，跳过嵌套类型
  let depth = 0;
  let i = braceStart;
  const fields = [];
  while (i < src.length) {
    const c = src[i];
    if (c === '{') {
      depth += 1;
      i += 1;
      continue;
    }
    if (c === '}') {
      depth -= 1;
      if (depth === 0) break;
      i += 1;
      continue;
    }
    if (depth !== 1) {
      i += 1;
      continue;
    }
    // 尝试匹配字段：注解? 修饰符 类型 名称 [=...] ;
    const slice = src.slice(i);
    if (/^\/\//.test(slice) || /^\/\*/.test(slice)) {
      i += 1;
      continue;
    }
    const fieldMatch = slice.match(
      /^((?:@\w+(?:\((?:[^()]|\([^()]*\))*\))?\s*)*)((?:(?:public|protected|private|static|final|transient|volatile)\s+)*)([\w.<>,\s\[\]]+?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|;)/,
    );
    if (fieldMatch) {
      const mods = fieldMatch[2] || '';
      if (!/\bstatic\b/.test(mods)) {
        const typeExpr = fieldMatch[3].trim();
        const name = fieldMatch[4];
        if (!typeExpr.includes('(') && name !== 'serialVersionUID') {
          const annoBlock = fieldMatch[1] || '';
          const schemaDesc = extractSchemaDescription(annoBlock);
          const req = inferFieldRequired(annoBlock);
          const mapped = mapJavaType(typeExpr);
          fields.push({
            name,
            ...mapped,
            required: req.required,
            requiredSource: req.requiredSource,
            description: schemaDesc || '',
          });
        }
      }
      i += fieldMatch[0].length;
      continue;
    }
    i += 1;
  }
  return { fields, extendsType, lombok };
}

function extractSchemaDescription(annoBlock) {
  const m = annoBlock.match(/@Schema\s*\(((?:[^()]|\([^()]*\))*)\)/);
  if (!m) return '';
  const dm = m[1].match(/description\s*=\s*"((?:\\.|[^"\\])*)"/);
  return dm ? dm[1] : '';
}

/**
 * 从字段注解推断是否必填。
 * 优先级：Bean Validation → @Schema(required/requiredMode) → 默认可选。
 * requiredSource:
 *   - validation：@NotNull / @NotBlank / @NotEmpty / @Nullable
 *   - schema：OpenAPI @Schema
 *   - default：无信号（Agent 须结合业务语义，禁止一律写「否」）
 */
function inferFieldRequired(annoBlock) {
  const block = annoBlock || '';
  // 注意：@ 非词字符，不能写 \b@NotNull；支持简单名与 FQCN
  if (/@(?:[\w.]+\.)?(?:NotNull|NotBlank|NotEmpty)\b/.test(block)) {
    return { required: true, requiredSource: 'validation' };
  }
  if (/@(?:[\w.]+\.)?Nullable\b/.test(block)) {
    return { required: false, requiredSource: 'validation' };
  }
  // OpenAPI @Schema / Jackson @JsonProperty(required=…)
  if (/\brequiredMode\s*=\s*(?:[\w.]+\.)?REQUIRED\b/.test(block)) {
    return { required: true, requiredSource: 'schema' };
  }
  if (/\brequiredMode\s*=\s*(?:[\w.]+\.)?NOT_REQUIRED\b/.test(block)) {
    return { required: false, requiredSource: 'schema' };
  }
  if (/\brequired\s*=\s*true\b/.test(block)) {
    return { required: true, requiredSource: 'schema' };
  }
  if (/\brequired\s*=\s*false\b/.test(block)) {
    return { required: false, requiredSource: 'schema' };
  }
  return { required: false, requiredSource: 'default' };
}

/** IC 公共包基类字段（业务仓通常无源码；与 references/project-conventions.md §3 对齐） */
const PLATFORM_BASE_FIELDS = {
  BaseEntity: [
    { name: 'creatorId', type: 'string', javaType: 'Long', required: false, requiredSource: 'platform', description: '创建者 ID（JSON 字符串）' },
    { name: 'createBy', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '创建者' },
    { name: 'createTime', type: 'string', javaType: 'LocalDateTime', required: false, requiredSource: 'platform', description: '创建时间' },
    { name: 'updateBy', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '更新者' },
    { name: 'updateTime', type: 'string', javaType: 'LocalDateTime', required: false, requiredSource: 'platform', description: '更新时间' },
    { name: 'tenantId', type: 'string', javaType: 'Long', required: false, requiredSource: 'platform', description: '租户 ID（JSON 字符串）' },
    { name: 'deleted', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '逻辑删除标志' },
  ],
  BaseBusinessEntity: [
    { name: 'orgCode', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '组织代码' },
    { name: 'invoiceSerial', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '单据编号' },
    { name: 'moduleCode', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '表单/模块编码' },
    {
      name: 'processInstanceId',
      type: 'string',
      javaType: 'Long',
      required: false,
      requiredSource: 'platform',
      description: '流程实例 ID（JSON 字符串）',
    },
    { name: 'reviewStatus', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '审批状态' },
    { name: 'text1', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '预留文本（null 不输出）' },
    { name: 'text2', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '预留文本（null 不输出）' },
    { name: 'text3', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '预留文本（null 不输出）' },
    { name: 'text4', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '预留文本（null 不输出）' },
    { name: 'text5', type: 'string', javaType: 'String', required: false, requiredSource: 'platform', description: '预留文本（null 不输出）' },
    { name: 'date1', type: 'string', javaType: 'LocalDate', required: false, requiredSource: 'platform', description: '预留日期（null 不输出）' },
    { name: 'date2', type: 'string', javaType: 'LocalDate', required: false, requiredSource: 'platform', description: '预留日期（null 不输出）' },
    { name: 'date3', type: 'string', javaType: 'LocalDate', required: false, requiredSource: 'platform', description: '预留日期（null 不输出）' },
    { name: 'date4', type: 'string', javaType: 'LocalDate', required: false, requiredSource: 'platform', description: '预留日期（null 不输出）' },
    { name: 'date5', type: 'string', javaType: 'LocalDate', required: false, requiredSource: 'platform', description: '预留日期（null 不输出）' },
    { name: 'number1', type: 'number', javaType: 'BigDecimal', required: false, requiredSource: 'platform', description: '预留数值（null 不输出）' },
    { name: 'number2', type: 'number', javaType: 'BigDecimal', required: false, requiredSource: 'platform', description: '预留数值（null 不输出）' },
    { name: 'number3', type: 'number', javaType: 'BigDecimal', required: false, requiredSource: 'platform', description: '预留数值（null 不输出）' },
    { name: 'number4', type: 'number', javaType: 'BigDecimal', required: false, requiredSource: 'platform', description: '预留数值（null 不输出）' },
    { name: 'number5', type: 'number', javaType: 'BigDecimal', required: false, requiredSource: 'platform', description: '预留数值（null 不输出）' },
  ],
  BaseShared: [
    { name: 'moduleCode', type: 'string', javaType: 'String', required: true, requiredSource: 'platform', description: '模块编码' },
    {
      name: 'taskUser',
      type: 'Record<string, string>',
      javaType: 'Map<String, String>',
      required: false,
      requiredSource: 'platform',
      description: '任务用户（提交流程时传入处理人）',
    },
    {
      name: 'action',
      type: 'string',
      javaType: 'OperationEnum',
      required: true,
      requiredSource: 'platform',
      description: '操作：DRAFT / SAVE / SUBMIT',
    },
    { name: 'orgCode', type: 'string', javaType: 'String', required: true, requiredSource: 'platform', description: '组织代码' },
    {
      name: 'attachments',
      type: 'object[]',
      javaType: 'List',
      required: false,
      requiredSource: 'platform',
      description: '附件分组：groupId、categories[]（categoryId、fileIds[]）',
    },
  ],
};

/**
 * 公共基类字段树（BaseDTO 含嵌套 `_shared`，禁止摊平）。
 * @param {string} simple
 * @returns {object[]|null}
 */
function platformBaseFields(simple) {
  if (simple === 'BaseEntity') {
    return PLATFORM_BASE_FIELDS.BaseEntity.map((f) => ({ ...f, fromPlatform: true }));
  }
  if (simple === 'BaseBusinessEntity') {
    return [
      ...PLATFORM_BASE_FIELDS.BaseEntity,
      ...PLATFORM_BASE_FIELDS.BaseBusinessEntity,
    ].map((f) => ({ ...f, fromPlatform: true }));
  }
  if (simple === 'BaseShared') {
    return PLATFORM_BASE_FIELDS.BaseShared.map((f) => ({ ...f, fromPlatform: true }));
  }
  if (simple === 'BaseDTO') {
    return [
      {
        name: '_shared',
        type: 'object',
        javaType: 'BaseShared',
        simple: 'BaseShared',
        required: true,
        requiredSource: 'platform',
        description: '共享上下文（BaseShared）；禁止摊平到请求体顶层',
        fromPlatform: true,
        fields: PLATFORM_BASE_FIELDS.BaseShared.map((f) => ({ ...f, fromPlatform: true })),
      },
      ...PLATFORM_BASE_FIELDS.BaseEntity,
      ...PLATFORM_BASE_FIELDS.BaseBusinessEntity,
    ].map((f) => (f.name === '_shared' ? f : { ...f, fromPlatform: true }));
  }
  return null;
}

function resolveType(indexBundle, typeName, { depth = 0, seen = new Set() } = {}) {
  const simple = simpleTypeName(typeName) || typeName;
  const mapped = mapJavaType(typeName);

  // 基本类型 / 无本地源码的集合
  if (['string', 'number', 'boolean', 'null'].includes(mapped.type) && !mapped.needs_source) {
    return { name: simple, ...mapped, fields: [], resolved: true };
  }
  if (mapped.type.endsWith('[]') && mapped.item) {
    const itemResolved = resolveType(indexBundle, mapped.item.javaType || mapped.item.simple, {
      depth: depth + 1,
      seen,
    });
    return {
      name: simple,
      type: mapped.type,
      javaType: mapped.javaType,
      item: itemResolved,
      fields: [],
      resolved: true,
    };
  }
  if (mapped.type.startsWith('Record<') && mapped.value) {
    const valResolved = resolveType(indexBundle, mapped.value.javaType || mapped.value.simple, {
      depth: depth + 1,
      seen,
    });
    return {
      name: simple,
      type: mapped.type,
      javaType: mapped.javaType,
      value: valResolved,
      fields: [],
      resolved: true,
    };
  }

  if (depth > 6) {
    return { name: simple, ...mapped, fields: [], resolved: false, needs_source: true, reason: 'max_depth' };
  }
  if (seen.has(simple)) {
    return { name: simple, type: 'object', javaType: typeName, fields: [], resolved: false, circular: true };
  }

  const entry = pickType(indexBundle.index, typeName);
  if (!entry) {
    const platformFields = platformBaseFields(simple);
    if (platformFields) {
      return {
        name: simple,
        type: 'object',
        javaType: typeName,
        fields: platformFields,
        resolved: true,
        fromPlatform: true,
        description: `IC 公共包 ${simple}（本仓库无源码；字段来自项目约定）`,
      };
    }
    return {
      name: simple,
      type: mapped.type || 'object',
      javaType: typeName,
      fields: [],
      resolved: false,
      needs_source: true,
      reason: 'not_found_in_srcRoot',
    };
  }

  seen.add(simple);
  const parsed = parseFieldsFromClass(entry);
  let fields = [...parsed.fields];
  let extendsUnresolved = null;

  if (parsed.extendsType) {
    const parentSimple = simpleTypeName(parsed.extendsType);
    const parent = resolveType(indexBundle, parsed.extendsType, { depth: depth + 1, seen: new Set(seen) });
    if (parent.resolved && parent.fields) {
      fields = [...parent.fields, ...fields];
    } else {
      const injected = platformBaseFields(parentSimple);
      if (injected) {
        fields = [...injected, ...fields];
      } else {
        // 不要注入 _extends_* / _(继承)_ 这类假字段名；留给文档说明或项目规范
        extendsUnresolved = {
          simple: parentSimple,
          javaType: parsed.extendsType,
          needs_source: true,
          description: `继承自 ${parsed.extendsType}（本仓库无源码；字段见项目规范）`,
        };
      }
    }
  }

  // 源码在本地时，充实嵌套 / 集合元素类型
  fields = fields.map((f) => {
    if (f.needs_source && f.simple && indexBundle.index.has(f.simple) && depth < 4) {
      const nested = resolveType(indexBundle, f.simple, { depth: depth + 1, seen: new Set(seen) });
      if (nested.resolved) {
        return { ...f, needs_source: false, fields: nested.fields, type: f.type };
      }
    }
    if (f.item && f.item.simple && indexBundle.index.has(f.item.simple) && depth < 4) {
      const nested = resolveType(indexBundle, f.item.simple, { depth: depth + 1, seen: new Set(seen) });
      if (nested.resolved) {
        return {
          ...f,
          item: { ...f.item, needs_source: false, fields: nested.fields, resolved: true },
        };
      }
    }
    return f;
  });

  return {
    name: simple,
    fqcn: entry.fqcn,
    file: entry.relative,
    type: 'object',
    javaType: entry.fqcn,
    lombok: parsed.lombok,
    extendsType: parsed.extendsType,
    extendsUnresolved,
    fields,
    resolved: true,
  };
}

function resolveTypes(srcRoot, typeNames) {
  const bundle = buildTypeIndex(srcRoot);
  const types = {};
  for (const name of typeNames) {
    const trimmed = name.trim();
    if (!trimmed) continue;
    types[trimmed] = resolveType(bundle, trimmed);
  }
  return { srcRoot: bundle.root, types };
}

export {
  buildTypeIndex,
  resolveTypes,
  resolveType,
  platformBaseFields,
  inferFieldRequired,
};
