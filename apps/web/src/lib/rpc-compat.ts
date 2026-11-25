import {
  fromJson,
  toJson,
  ScalarType,
  type DescMessage,
  type DescField,
  type Message,
  type JsonValue,
  type JsonObject
} from '@bufbuild/protobuf';
const special = (s: DescMessage) =>
  ['google.protobuf.Struct', 'google.protobuf.Value', 'google.protobuf.Timestamp'].includes(
    s.typeName
  );
const name = (f: DescField) =>
  f.parent.typeName.startsWith('sellerops.auth.') ? f.jsonName : f.name;
const enumText = (f: DescField, value: string) => {
  const prefix = f.enum?.name.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toUpperCase() + '_';
  return value.startsWith(prefix) ? value.slice(prefix.length).toLowerCase() : value;
};
export function prepareRPCJSON(schema: DescMessage, value: JsonValue): JsonValue {
  if (special(schema)) return value;
  if (value === null || Array.isArray(value) || typeof value !== 'object')
    throw new Error('Expected JSON object');
  const out: JsonObject = {},
    extra: JsonObject = {},
    absent: string[] = [],
    known = new Set<string>();
  for (const field of schema.fields) {
    if (['legacy_fields', 'omitted_fields'].includes(field.name)) continue;
    const key = name(field);
    known.add(key);
    if (!Object.hasOwn(value, key)) {
      absent.push(key);
      continue;
    }
    const v = value[key];
    if (
      ['expected_data_revision', 'expected_review_revision', 'expected_monthly_revision'].includes(
        key
      ) &&
      typeof v === 'string'
    ) {
      extra[key] = v;
      continue;
    }
    if (v === null) {
      extra[key] = null;
      continue;
    }
    const one = (item: JsonValue): JsonValue => {
      if (field.enum && typeof item === 'string') {
        const found = field.enum.values.find(
          (e) => enumText(field, e.name) === item || e.name === item
        );
        if (found) return found.name;
        if (item === '') {
          extra[key] = '';
          return null;
        }
      }
      if (field.message && !special(field.message)) return prepareRPCJSON(field.message, item);
      return item;
    };
    const result =
      field.fieldKind === 'list' && Array.isArray(v)
        ? v.map(one)
        : field.fieldKind === 'map' && v && typeof v === 'object' && !Array.isArray(v)
          ? Object.fromEntries(Object.entries(v).map(([k, item]) => [k, one(item)]))
          : one(v);
    if (result !== null || v === null) out[field.jsonName] = result;
    if (field.message?.typeName === 'google.protobuf.Timestamp') extra[key] = v;
  }
  for (const [key, v] of Object.entries(value)) if (!known.has(key)) extra[key] = v;
  if (schema.fields.some((f) => f.name === 'legacy_fields')) out.legacyFields = extra;
  if (schema.fields.some((f) => f.name === 'omitted_fields')) out.omittedFields = absent;
  return out;
}
export function legacyRPCJSON(schema: DescMessage, message: Message): JsonObject {
  return restore(schema, toJson(schema, message, { alwaysEmitImplicit: true })) as JsonObject;
}
function restore(schema: DescMessage, value: JsonValue): JsonValue {
  if (special(schema)) return value;
  if (value === null || Array.isArray(value) || typeof value !== 'object') return value;
  const out: JsonObject = {};
  for (const field of schema.fields) {
    if (
      ['legacy_fields', 'omitted_fields'].includes(field.name) ||
      !Object.hasOwn(value, field.jsonName)
    )
      continue;
    const v = value[field.jsonName];
    const one = (item: JsonValue): JsonValue => {
      if (field.enum && typeof item === 'string') return enumText(field, item);
      if (field.message) return restore(field.message, item);
      if (
        field.scalar &&
        [
          ScalarType.INT64,
          ScalarType.UINT64,
          ScalarType.SINT64,
          ScalarType.FIXED64,
          ScalarType.SFIXED64
        ].includes(field.scalar) &&
        typeof item === 'string'
      ) {
        const n = Number(item);
        if (!Number.isSafeInteger(n)) throw new Error('Integer exceeds JSON safe range');
        return n;
      }
      return item;
    };
    out[name(field)] =
      field.fieldKind === 'list' && Array.isArray(v)
        ? v.map(one)
        : field.fieldKind === 'map' && v && typeof v === 'object' && !Array.isArray(v)
          ? Object.fromEntries(Object.entries(v).map(([k, item]) => [k, one(item)]))
          : one(v);
  }
  const extra = value.legacyFields;
  if (extra && typeof extra === 'object' && !Array.isArray(extra)) Object.assign(out, extra);
  const absent = value.omittedFields;
  if (Array.isArray(absent)) for (const key of absent) if (typeof key === 'string') delete out[key];
  return out;
}
export function fromLegacyRPCJSON(schema: DescMessage, value: JsonValue): Message {
  return fromJson(schema, prepareRPCJSON(schema, value));
}
