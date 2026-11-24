import type { Json, Product } from './types';

// Concise UI summaries of the Nice 2026 goods headings (classes 01–34).
// https://nclpub.wipo.int/esen/pdf-download.pdf?dateInForce=20260101&lang=en&tab=class_headings
const classDescriptions: Record<string, string> = {
  '10': '医疗器械及按摩器具',
  '11': '照明、加热及供水设备',
  '12': '车辆及运输工具',
  '13': '火器、弹药及烟火',
  '14': '珠宝首饰及钟表',
  '15': '乐器及其配件',
  '16': '纸品、印刷品及文具',
  '17': '橡胶、塑料半成品及绝缘材料',
  '18': '皮革、箱包及雨伞',
  '19': '非金属建筑材料',
  '20': '家具、镜子及非金属容器',
  '21': '家用及厨房器具、容器',
  '22': '绳网、帐篷及纺织原料',
  '23': '纺织用纱线',
  '24': '织物、家用纺织品及床上用品',
  '25': '服装、鞋及帽',
  '26': '缝纫用品、饰品及假发',
  '27': '地毯、地垫及墙面覆盖物',
  '28': '玩具、游戏及运动器材',
  '29': '肉鱼、乳制品及加工食品',
  '30': '咖啡、茶、谷物食品及调味品',
  '31': '农业园艺产品、活体动植物及饲料',
  '32': '啤酒及无酒精饮料',
  '33': '含酒精饮料（啤酒除外）',
  '34': '烟草、烟具及电子烟',
  '01': '化学原料及工业化学品',
  '02': '颜料、油漆及防锈制剂',
  '03': '清洁制剂及化妆品',
  '04': '工业油脂、燃料及照明材料',
  '05': '药品、医用制剂及消毒用品',
  '06': '普通金属及金属制品',
  '07': '机器、机床及动力工具',
  '08': '手动工具及刀叉餐具',
  '09': '科学仪器、电子及安全设备'
};

export function trademarkOptionLabel(code: string): string {
  if (code === 'pending') return '待核实';
  const description = classDescriptions[code];
  return description ? `第${code}类（${description}）` : `第${code}类`;
}

function objectValue(value: Json | undefined): Record<string, Json> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value : undefined;
}

export function productTrademark(product: Pick<Product, 'specs'>): {
  label: string;
  title: string;
} {
  const specs = product.specs || {};
  const legacy = objectValue(objectValue(specs.legacy_site_snapshot)?.p);
  const direct = typeof specs.trademark_class === 'string' ? specs.trademark_class.trim() : '';
  const raw = direct || (typeof legacy?.trademark_class === 'string' ? legacy.trademark_class : '');
  const codes = [...new Set([...raw.matchAll(/第(\d{2})类/g)].map((match) => match[1]))].filter(
    (code) => classDescriptions[code]
  );
  if (!codes.length) return { label: '商标待核实', title: raw || '暂无商标分类' };
  return {
    label:
      codes.map((code) => `第${code}类`).join(' / ') + (raw.includes('待核实') ? ' · 待核实' : ''),
    title: codes.map(trademarkOptionLabel).join('；') + (raw.includes('待核实') ? '；' + raw : '')
  };
}
