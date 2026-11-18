"""Shared SellerSprite grouping: purpose, main material, structure and compatibility."""

import re, collections
from urllib.parse import urlparse, parse_qs

RULESET_VERSION = "2026-10-26-purpose-material-structure-v1"


def hit(pattern, text):
    return bool(re.search(pattern, text, re.I))


def categories(value):
    return [s.strip() for s in str(value).replace("\r", "").split("\n") if s.strip()]


def attributes(raw):
    return dict(
        (k.strip().lower(), v.strip())
        for k, v in re.findall(
            r"(?:^|\|)\s*([^:|]+):([^|]*)", str(raw.get("详细参数") or "")
        )
    )


MATERIALS = [
    (
        "钛/塑料复合",
        r"titanium\s*[/+&]\s*(?:pp|plastic)|titanium.*(?:double.sided|dual.sided).*(?:pp|plastic)",
    ),
    ("钛", r"\btitanium\b"),
    ("玻璃", r"\bglass\b"),
    ("硅胶", r"\bsilicone\b"),
    ("不锈钢", r"stainless|\bsus\s*\d"),
    ("碳钢", r"carbon steel"),
    ("钢", r"\bsteel\b"),
    ("铝", r"alumin"),
    ("铜", r"\bcopper\b"),
    ("竹", r"\bbamboo\b"),
    ("相思木", r"\bacacia\b"),
    ("柚木", r"\bteak\b"),
    ("核桃木", r"\bwalnut\b"),
    ("木", r"\bwood(?:en)?\b"),
    ("陶瓷", r"ceramic|porcelain"),
    ("亚克力", r"acrylic|plexiglass"),
    ("丁腈", r"nitrile"),
    ("乳胶", r"latex"),
    ("橡胶", r"\brubber\b"),
    ("天然丝瓜络", r"loofa|loofah|luffa"),
    ("猪鬃", r"boar bristle"),
    ("超细纤维", r"microfiber|microfibre"),
    ("棉", r"\bcotton\b"),
    ("亚麻", r"\blinen\b"),
    ("涤纶", r"polyester|spandex"),
    ("尼龙", r"\bnylon\b"),
    ("毛毡", r"\bfelt\b"),
    ("缎面", r"\bsatin\b"),
    ("真丝", r"\bsilk\b"),
    ("PEVA", r"\bpeva\b"),
    ("EVA", r"\beva\b"),
    ("PVC/乙烯基", r"\bpvc\b|\bvinyl\b"),
    ("纸", r"\bpaper\b|cardstock|cardboard|manila"),
    (
        "塑料",
        r"plastic|polypropylene|polyethylene|\bpp\b|\bpet\b|\babs\b|\bpe\b|\bps\b",
    ),
    ("金属", r"\bmetal\b|\biron\b"),
    ("织物", r"\bfabric\b|\bcloth\b"),
]


def material(title, raw, kind=""):
    attrs = attributes(raw)
    preferred = (
        attrs.get("blade material", "")
        if kind == "刀具"
        else attrs.get("bristle material", "")
        if kind == "刷具"
        else attrs.get("material", "")
        or attrs.get("fabric type", "")
        or attrs.get("frame material", "")
    )
    # The title can specify a compound main body missing in the structured material.
    if hit(MATERIALS[0][1], title):
        return MATERIALS[0][0]
    if not preferred and hit(r"faux linen|linen.look", title):
        return "仿亚麻织物（原料未注明）"
    for text in [preferred, title]:
        for name, pattern in MATERIALS:
            if hit(pattern, text):
                return name
    return "材质未注明"


SIMPLE = {
    "Packing Tape": "封箱胶带",
    "Self-Stick Note Pads": "便利贴",
    "Self-Stick Notes": "便利贴",
    "Pants Hangers": "裤架",
    "Standard Hangers": "衣架",
    "Manila File Folders": "纸质文件夹",
    "Colored File Folders": "纸质文件夹",
    "Project Folders": "项目文件夹",
    "Transparent Tape": "透明办公胶带",
    "Bubble Wrap": "气泡包装材料",
    "Measuring Cups": "量杯",
    "Measuring Spoons": "量勺",
    "Cooking Tongs": "烹饪夹",
    "Peelers": "削皮器",
    "Meat & Poultry Tenderizers": "手动嫩肉工具",
    "Knife Sharpeners": "手动磨刀器",
    "Manual Can Openers": "手动开罐器",
    "Pizza Cutters": "披萨切刀",
    "Garlic Presses": "压蒜器",
    "Cooking Spoons": "烹饪勺",
    "Spatulas": "烹饪铲",
    "Colanders": "沥水篮",
    "Whisks": "手动打蛋器",
    "Kitchen Shears": "厨房剪刀",
    "Graters": "食材刨丝器",
    "Cheese Graters": "食材刨丝器",
    "Food Strainers": "食品滤网",
    "Funnels": "分装漏斗",
    "Manual Juicers": "手动榨汁器",
    "Nut Crackers": "坚果夹",
    "Splatter Screens": "防油溅网",
    "Seasoning Injectors": "食物腌料注射器",
    "Meat & Poultry Basters": "食物滴油工具",
    "Jar Openers": "开瓶罐工具",
    "Chef's Knives": "主厨刀",
    "Bread Knives": "面包刀",
    "Kitchen Knife Sets": "厨房刀具套装",
    "Block Knife Sets": "带刀座厨房刀具套装",
    "Knife Block Sets": "带刀座厨房刀具套装",
    "Cook's Tool & Gadget Sets": "厨房手工具套装",
    "Cooking Utensil Sets": "烹饪用具套装",
    "Oven Mitts": "隔热手套",
    "Trivets": "隔热垫",
    "Cookbook Stands": "食谱支架",
    "Dish Drying Mats": "餐具沥水垫",
    "Bread Boxes": "面包收纳盒",
    "Food Jars & Canisters": "食品储存罐",
    "Cereal Containers": "干粮储存罐",
    "Spice Cellars": "调味料储存罐",
    "Refillable Squeeze Bottles": "可挤压分装瓶",
    "Refillable Cosmetic Spray Bottles": "化妆品空喷瓶",
    "Insulated Bottles": "保温饮料瓶",
    "Tumblers": "饮料杯",
    "Luggage Tags": "行李牌",
    "Hair Drying Towels": "干发巾",
    "Bed Pillow Protectors": "枕芯保护套",
    "Bed Pillow Pillowcases": "枕套",
    "Fitted Bed Sheets": "床笠",
    "Sheet & Pillowcase Sets": "床单枕套套装",
    "Bath Towels": "浴巾",
    "Bath Towel Sets": "浴巾套装",
    "Hand Bath Towels": "手巾",
    "Bath Washcloths": "洗澡方巾",
    "Bath Loofahs": "沐浴丝瓜络或浴球",
    "Body Exfoliating Gloves": "去角质手套",
    "Bath Pillows": "浴缸靠枕",
    "Hair Bonnets": "护发睡帽",
    "Hair Elastics & Ties": "发圈",
    "Women's Fashion Headbands": "发箍",
    "Hair Combs": "梳子",
    "Makeup Blotting Paper": "吸油纸",
    "Makeup Brush Sets & Kits": "化妆刷套装",
    "Kabuki Brushes": "卡布基化妆刷",
    "Dish Scouring Pads": "洗碗百洁垫",
    "Household Surfaces Scouring Pads & Sticks": "清洁擦垫或擦棒",
    "Household Squeegees": "家用刮水器",
    "Laundry Bags": "洗衣袋",
    "Reusable Produce Bags": "可重复使用果蔬袋",
    "Moving Bags": "搬家收纳袋",
    "Cosmetic Bags": "化妆包",
    "Pen, Pencil & Marker Cases": "文具收纳袋",
    "Toiletry Bags": "洗漱包",
    "Hat Boxes": "帽子收纳盒",
    "Boot & Shoe Boxes": "鞋盒",
    "Lidded Home Storage Bins": "带盖储物箱",
    "Under-Bed Storage": "床底收纳容器",
    "Drawer Organizers": "抽屉分隔收纳",
    "Storage Boxes": "储物盒",
    "Laundry Hampers": "脏衣篮",
    "Laundry Baskets": "洗衣篮",
    "Laundry Sorters": "分类洗衣篮",
    "Garment Racks": "衣物挂架",
    "Free Standing Shoe Racks": "落地鞋架",
    "Shoe Slots": "鞋位收纳架",
    "Spice Racks": "调料收纳架",
    "Coffee Pod Stands": "咖啡胶囊架",
    "Coffee Pod Drawers": "咖啡胶囊抽屉",
    "Stacking Can Dispensers": "饮料罐堆叠收纳架",
    "Pot Lid Holders": "锅盖收纳架",
    "Shelf Liners": "搁板衬垫",
    "Dish Racks": "餐具沥水架",
    "Paper Towel Holders": "厨房纸巾架",
    "Utility Hooks": "通用挂钩",
    "Hat Racks": "帽子挂架",
    "Under-Sink Organizers": "水槽下收纳架",
    "Cosmetic Display Cases": "化妆品展示收纳",
    "Standing Dry Racks": "落地晾衣架",
    "Shower Curtain Sets": "浴帘套装",
    "Shower Curtain Liners": "浴帘内衬",
    "Shower Curtain Rods": "浴帘杆",
    "Badge Holders": "证件挂件",
    "Letter Trays & Stacking Supports": "桌面文件托盘",
    "Pencil Holders & Pen Holders": "笔筒",
    "Binder Index Dividers": "活页分类隔页",
    "Round Ring Binders": "圆环活页夹",
    "Expanding Files & Wallets": "风琴文件夹",
    "Clipboards": "写字夹板",
    "Desk Pads & Blotters": "桌垫",
    "Mouse Pads": "鼠标垫",
    "Laser & Inkjet Printer Labels": "打印机标签纸",
    "All-Purpose Labels": "通用标签",
    "Supply Organizers": "文具收纳",
    "Refrigerator Magnets": "冰箱磁贴",
    "Magnetic Tape": "磁性胶带",
    "Clothes Pins": "晾衣夹",
    "Travel Packing Organizers": "行李收纳袋",
    "Hair Brushes": "发刷",
    "Bath & Body Brushes": "沐浴身体刷",
    "Household Cleaning Brushes": "家用清洁刷",
    "Kitchen Brushes": "厨房清洁刷",
    "Fruit & Vegetable Cleaning Brushes": "果蔬清洁刷",
    "Household Push Brooms": "家用扫帚",
    "Household Dustpans": "家用簸箕",
    "Household Dust Mops": "除尘拖把",
    "Household Replacement Mop Pads": "拖把替换布",
    "Dryer Balls": "烘干机球",
    "Composition Notebooks": "缝线装订笔记本",
    "Subject Notebooks": "分科笔记本",
    "Spiral Notebooks": "螺旋装订笔记本",
    "Hardcover Executive Notebooks": "精装笔记本",
}


def refined(x, headers):
    t = x["title"].lower()
    raw = dict(zip(headers, x["source"]["values"]))
    cats = categories(x["category"])
    cat = next((c for c in cats if c in SIMPLE), cats[0] if cats else "")
    m = material(
        t,
        raw,
        "刀具" if "Kniv" in cat or "Knife" in cat else "刷具" if "Brush" in cat else "",
    )
    base = None
    parts = []
    if any(
        c in cats
        for c in [
            "Food Container Sets",
            "Container Sets",
            "Individual Household Food Containers",
            "Bento Boxes",
            "Food Dispensers",
            "Kitchen Storage Accessories",
            "Kitchen Storage & Organization Accessories",
            "Kitchen Accessories",
        ]
    ):
        if hit(r"(?:grocery|plastic|shopping) bag (?:holder|dispenser)|bag saver", t):
            base = "购物袋收纳分配器"
        elif hit(r"juice bottles?|juicing bottles?|ginger.*bottles?", t):
            base = "饮料分装瓶"
        elif hit(r"(?:deli|soup) (?:food )?containers?", t):
            base = "熟食汤品分装盒"
        elif hit(r"(?:soup|portion).*freez|freez.*(?:soup|portion)|freezer tray", t):
            base = "冻汤分装模具"
        elif hit(
            r"(?:fridge|refrigerator|storage|clear).*\bbins?\b|bins?.*(?:fridge|refrigerator)",
            t,
        ):
            base = "冰箱或橱柜分区收纳盒"
        elif hit(
            r"flour|sugar|cereal|dry food|pantry.*(?:canister|food.*container)", t
        ):
            base = "干粮密封储存罐"
        elif hit(r"bento|meal prep|lunch containers?|food storage containers?", t):
            base = "餐食保鲜盒"
        elif hit(r"condiment|sauce|dipping", t):
            base = "调料分装杯"
        elif hit(r"egg.*(?:holder|storage|organizer)", t):
            base = "鸡蛋收纳器"
        elif hit(r"fridge|refrigerator", t):
            base = "冰箱分区收纳盒"
        elif hit(r"bag.*(?:organizer|storage)|organizer.*bag", t):
            base = "包装袋收纳器"
        if base:
            n = re.search(r"\b([2-5])\s*(?:compartment|section)", t)
            parts.append(n[1] + "分隔" if n else "无明确分隔")
            parts.append(
                "旋拧盖"
                if hit(r"screw|twist.top|twist.lid", t)
                else "卡扣盖"
                if hit(r"locking|snap|latch", t)
                else "普通盖"
            )
    elif "Tablecloths" in cats:
        base = "桌布"
        parts.append(
            "弹力贴合" if hit(r"elastic|fitted|stretch|spandex", t) else "普通披覆"
        )
        parts.append("一次性" if hit(r"disposable", t) else "可重复用或未注明")
        parts.append("法兰绒背衬" if hit("flannel", t) else "普通背衬")
    elif "Mounting Tape" in cats:
        base = "安装固定胶带"
        parts.append(
            "魔术贴"
            if hit(r"hook.and.loop|velcro", t)
            else "泡棉胶带"
            if hit(r"foam", t)
            else "纳米胶带"
            if hit(r"nano", t)
            else "普通双面胶"
        )
        parts.append(
            "预切胶片" if hit(r"pre.cut|squares|dots|adhesive pads", t) else "卷材"
        )
    elif any("Ice Cube Molds & Trays" == c for c in cats):
        base = "冰块模具"
        parts.append(
            "冰球"
            if hit(r"\bsphere|\bball", t)
            else "冰棒"
            if hit("popsicle|ice pop", t)
            else "冰柱"
            if hit("stick|cylinder", t)
            else "碎小冰"
            if hit(r"pebble|nugget|mini|small ice", t)
            else "方冰"
        )
        parts.append("保温透明冰结构" if hit("clear ice|insulated", t) else "普通模具")
        parts.append(
            "收纳盒套装" if hit("bin|storage container|bucket", t) else "单托盘或模具"
        )
    elif "Cutting Boards" in cats:
        base = "厨房砧板"
        parts.append("柔性" if hit("flexible|mats", t) else "硬板")
        parts.append(
            "带容器或套盆" if hit(r"container|collapsible.*sink", t) else "普通板"
        )
        parts.append(
            "带刀具" if hit(r"with.*kniv|with.*knife", t) else "不含刀具或未注明"
        )
    elif "Tag Attacher Guns" in cats:
        base = (
            "衣物临时缝合枪"
            if hit("stitch|clothing fix|microstitch", t)
            else "服装吊牌枪"
        )
        parts.append("微针" if hit("micro|ultra.fine", t) else "普通针")
    elif any(
        c in cats
        for c in ["False Eyelashes", "False Lashes", "False Eyelash & Adhesive Sets"]
    ):
        base = "假睫毛"
        parts.append(
            "磁吸"
            if hit("magnetic", t)
            else "自粘"
            if hit("self.adhesive|self.adhesive|no glue|glueless|press.on", t)
            else "需外加胶或未注明"
        )
        parts.append("簇状" if hit("cluster|individual", t) else "条状")
        parts.append("下睫毛" if hit("bottom|lower lash", t) else "上睫毛或通用")
    elif "Oil Dispensing Oil Sprayers" in cats:
        base = "烹饪油空分装器"
        parts.append(
            "喷倒两用"
            if hit("2.in.1|pour.*spray|spray.*pour", t)
            else "喷雾型"
            if hit("spray|mister", t)
            else "倾倒型"
        )
    elif "Binder Sheets, Card & Photo Sleeves" in cats:
        base = (
            "卡片多格内页"
            if hit("card.*sleeve|pockets.*card|card.*pocket", t)
            else "文件保护内页"
        )
        n = re.search(r"\b(4|9|12|16|18)\s*pocket", t)
        parts.append(n[1] + "格" if n else "格数未注明")
    elif cat in SIMPLE:
        base = SIMPLE[cat]
    if base is None:
        return None
    if cat in [
        "Storage Boxes",
        "Supply Organizers",
        "Drawer Organizers",
        "Cosmetic Display Cases",
        "Pencil Holders & Pen Holders",
    ]:
        for pattern, label in [
            (r"jewel|earring|necklace", "首饰收纳"),
            (r"photo|picture", "照片收纳"),
            (r"tea bag", "茶包收纳"),
            (r"cosmetic|makeup", "化妆品收纳"),
            (r"pen|pencil|stationery", "文具收纳"),
            (r"\bshoe|\bboot", "鞋类收纳"),
            (r"cable|cord", "线缆收纳"),
            (r"cutlery|silverware|utensil", "餐具收纳"),
        ]:
            if hit(pattern, t):
                base = label
                break
        else:
            if cat in ["Storage Boxes", "Supply Organizers", "Drawer Organizers"]:
                return None
    if base in ["厨房刀具套装", "带刀座厨房刀具套装"]:
        parts.append(
            "磁吸刀座"
            if hit("magnetic", t)
            else "插槽刀座"
            if hit("block", t)
            else "护套收纳"
            if hit("guard|sheath", t)
            else "普通收纳"
        )
        parts.append(
            "自带磨刀结构" if hit("built.in.*sharp|self.sharpen", t) else "普通刀组"
        )
    if cat in [
        "Household Cleaning Brushes",
        "Kitchen Brushes",
        "Fruit & Vegetable Cleaning Brushes",
    ]:
        for pattern, label in [
            (r"bottle|water bottle", "瓶杯刷"),
            (r"toilet", "马桶刷"),
            (r"grout|crevice|gap", "缝隙清洁刷"),
            (r"shoe|sneaker", "鞋刷"),
            (r"dryer vent|dryer lint", "烘干机管道清洁刷"),
            (r"drain", "排水口刷"),
            (r"vegetable|potato|fruit", "果蔬刷"),
            (r"dish|pot pan|pots and pans", "锅碗清洁刷"),
        ]:
            if hit(pattern, t):
                base = label
                break
    if cat in ["Cooking Spoons", "Spatulas", "Cooking Tongs"]:
        parts.append(
            "漏孔结构" if hit("slotted|straining|draining", t) else "实面或未注明"
        )
        parts.append("刮刀" if hit("scraper|scraping|batter", t) else "普通烹饪头")
    if cat in [
        "Colanders",
        "Food Strainers",
        "Manual Juicers",
        "Meat & Poultry Tenderizers",
        "Knife Sharpeners",
        "Peelers",
        "Pizza Cutters",
        "Garlic Presses",
    ]:
        parts.append("可折叠" if hit("collapsible|foldable", t) else "固定结构")
        parts.append("转轴摇柄" if hit("rotary|crank|rotate", t) else "直接手动")
        parts.append(
            "台面架式" if hit("stand|countertop|table.top", t) else "手持或未注明"
        )
    if cat in ["Hair Brushes", "Hair Combs"]:
        parts.append(
            "卷发塑形"
            if hit("curl|defin|edgelift", t)
            else "圆筒造型"
            if hit("round|barrel", t)
            else "头皮按摩"
            if hit("scalp|massag", t)
            else "解结"
            if hit("detangl|wet brush", t)
            else "普通梳理"
        )
        parts.append("通风结构" if hit("vented|vent brush", t) else "普通刷体")
        if hit("curl|defin|edgelift", t):
            parts.append("蓬松造型" if hit("volume|volumiz", t) else "卷纹定义")
    if cat in [
        "Bath & Body Brushes",
        "Household Cleaning Brushes",
        "Kitchen Brushes",
        "Fruit & Vegetable Cleaning Brushes",
    ]:
        parts.append(
            "长柄" if hit("long handle|long.handle|back scrub", t) else "手持短柄"
        )
        parts.append("双面" if hit("double.sided|dual.sided", t) else "单面或未注明")
        parts.append("注液式" if hit("dispensing|soap.dispenser", t) else "普通刷体")
    if cat in ["Pants Hangers", "Standard Hangers"]:
        parts.append(
            "多层"
            if hit("multi.layer|multi.tier|5.layer|5.tier|s.shape", t)
            else "单层"
        )
        parts.append("夹式" if hit("clips|clamp|clip hanger", t) else "普通挂杆")
        parts.append("防滑绒面" if hit("velvet", t) else "普通表面")
    if cat in [
        "Subject Notebooks",
        "Spiral Notebooks",
        "Composition Notebooks",
        "Hardcover Executive Notebooks",
    ]:
        parts.append(
            "方格"
            if hit("graph|grid|quadrille", t)
            else "点阵"
            if hit("dotted|dot grid", t)
            else "空白"
            if hit("unruled|blank paper", t)
            else "横线"
        )
        parts.append(
            "活页可拆" if hit("loose.leaf|refillable|discbound", t) else "固定页"
        )
    if cat in [
        "Dish Racks",
        "Under-Sink Organizers",
        "Spice Racks",
        "Hat Racks",
        "Free Standing Shoe Racks",
        "Garment Racks",
        "Cosmetic Display Cases",
        "Standing Dry Racks",
        "Paper Towel Holders",
        "Coffee Pod Stands",
        "Pot Lid Holders",
    ]:
        parts.append("伸缩" if hit("expandable|extendable|telescop", t) else "固定尺寸")
        parts.append(
            "卷折"
            if hit("roll.up|roll up", t)
            else "可折叠"
            if hit("foldable|collapsible", t)
            else "固定结构"
        )
        parts.append(
            "壁挂"
            if hit("wall.mount|wall mounted", t)
            else "门背"
            if hit("over.the.door|over door", t)
            else "柜下吊挂"
            if hit("under.cabinet", t)
            else "水槽上方"
            if hit("over.the.sink|over sink", t)
            else "水槽内"
            if hit("in.sink|in sink", t)
            else "落地或台面"
        )
        parts.append("抽拉" if hit("pull.out|slide.out|sliding", t) else "非抽拉")
        parts.append("旋转" if hit("rotating|turntable|lazy susan", t) else "非旋转")
        n = re.search(r"\b([2-5])\s*[- ]?(?:tier|layer)", t)
        parts.append(n[1] + "层" if n else "单层或未注明")
    if cat in [
        "Utility Hooks",
        "Paper Towel Holders",
        "Hat Racks",
        "Shower Curtain Rods",
    ]:
        parts.append(
            "吸盘"
            if hit("suction", t)
            else "磁吸"
            if hit("magnetic", t)
            else "粘贴"
            if hit("adhesive|no.drill|command", t)
            else "螺钉"
            if hit("screw|drill", t)
            else "免钉或未注明"
        )
    if cat == "Badge Holders":
        parts.append(
            "伸缩卷线"
            if hit("retractable|reel", t)
            else "挂绳"
            if hit("lanyard", t)
            else "卡套"
        )
        parts.append(
            "封闭卡套" if hit("card holder|id holder|waterproof", t) else "挂件或未注明"
        )
    if cat == "Travel Packing Organizers":
        parts.append("压缩拉链" if hit("compression|compressible", t) else "普通收纳")
        parts.append("真空" if hit("vacuum", t) else "非真空")
    if cat in [
        "Food Jars & Canisters",
        "Cereal Containers",
        "Spice Cellars",
        "Bread Boxes",
    ]:
        parts.append(
            "旋拧盖"
            if hit("screw|twist", t)
            else "翻盖"
            if hit("flip.top|hinge|roll.top", t)
            else "普通盖"
        )
        parts.append("真空密封" if hit("vacuum", t) else "普通密封")
    if cat in ["Makeup Brush Sets & Kits", "Kabuki Brushes"]:
        parts.append("伸缩收纳" if hit("retractable", t) else "普通刷柄")
        parts.append("旅行收纳套装" if hit("travel|case", t) else "日常套装")
    if cat in ["Mandolines", "Seasoning & Spice Choppers"]:
        return None
    if cat in ["Bath Loofahs"]:
        parts.append("长柄" if hit("long handle|back scrub", t) else "手持")
        parts.append("扁片" if hit("pad|flat|slice", t) else "球状或块状")
    if cat in [
        "Lidded Home Storage Bins",
        "Under-Bed Storage",
        "Boot & Shoe Boxes",
        "Storage Boxes",
        "Cosmetic Bags",
        "Pen, Pencil & Marker Cases",
        "Toiletry Bags",
    ]:
        parts.append("可折叠" if hit("collapsible|foldable", t) else "固定结构")
        parts.append("硬壳" if hit("hard.shell|hard case", t) else "普通结构")
        parts.append("带轮" if hit("wheels|rolling", t) else "无轮或未注明")
        parts.append("拉链" if hit("zipper|zippered", t) else "非拉链")
        parts.append("可堆叠" if hit("stackable", t) else "未注明堆叠")
    if cat in ["Disposable Drinking Straws", "Reusable Drinking Straws"]:
        parts.append("弯头" if hit("bend|flexible", t) else "直管")
        parts.append("珍珠奶茶粗管" if hit("boba|bubble tea|jumbo", t) else "普通吸管")
    # Necessary compatibility systems remain independent even for passive accessories.
    systems = []
    for pattern, label in [
        (r"oral.?b", "Oral-B"),
        (r"sonicare", "Sonicare"),
        (r"swiffer.*powermop|power ?mop", "Swiffer PowerMop"),
        (r"swiffer.*sweeper", "Swiffer Sweeper"),
        (r"o.?cedar.*(?:2.tank|rinseclean)", "O-Cedar双桶"),
        (r"o.?cedar", "O-Cedar"),
        (r"vacmop", "Shark VACMOP"),
        (r"narwal", "Narwal"),
        (r"xxflower", "XXFLOWER"),
    ]:
        if hit(pattern, t):
            systems.append(label)
    if systems:
        parts.append("适配" + systems[0])
    return {
        "direction": " / ".join([base, m] + list(dict.fromkeys(parts))),
        "basis": "用途："
        + base
        + "；主体材质："
        + m
        + "；结构："
        + "、".join(dict.fromkeys(parts)),
        "rule": "结构化用途材质规则",
    }


COLORS = r"black|white|blue|red|pink|green|yellow|purple|grey|gray|orange|brown|beige|ivory|teal|navy|silver|gold|clear|transparent|assorted|multicolor|colour|color|cream|champagne"


def fingerprint(x, headers):
    raw = dict(zip(headers, x["source"]["values"]))
    t = x["title"].lower()
    brand = str(raw.get("品牌") or "").lower()
    if len(brand) > 1:
        t = t.replace(brand, " ")
    t = re.sub(r'\b\d+(?:\.\d+)?\s*["”″]\s*[x×]\s*\d+(?:\.\d+)?\s*["”″]?', " ", t)
    t = re.sub(
        r"\b(\d+)\s*[- ]?(compartment|tier|layer|ring|pocket|ply|blade|prong)s?\b",
        lambda m: " structural" + m[2] + "".join(chr(97 + int(c)) for c in m[1]) + " ",
        t,
    )
    t = re.sub(r"\b(?:" + COLORS + r")\b", " ", t)
    t = re.sub(
        r"\b\d+(?:[.,/-]\d+)*(?:\s*[x×]\s*\d+(?:\.\d+)?)*(?:\s*(?:inch(?:es)?|in|ft|feet|cm|mm|ml|liters?|litres?|oz|ounces?|gallons?|yards?|yds?|packs?|counts?|ct|pcs?|pieces?|rolls?|sheets?|pages?|lb|lbs|sq|g|kg))?\b",
        " ",
        t,
    )
    t = re.sub(
        r"\b(?:pack of|set of|count|pack|pcs|pieces|rolls|sheets|pages|size|large|small|medium|extra large|xl|xxl)\b",
        " ",
        t,
    )
    tokens = re.findall(r"[a-z]+[a-z0-9]*|[0-9]+[a-z][a-z0-9]*", t)
    return " ".join(tokens)


def assign(audit, headers):
    eligible = [x for x in audit if x["status"] == "eligible"]
    records = {}
    fallback = collections.defaultdict(list)
    for x in eligible:
        r = refined(x, headers)
        if r:
            records[x["asin"]] = r
            continue
        if not x["direction"].startswith("未校准方向"):
            old = x["direction"]
            t = x["title"].lower()
            parts = []
            if "垃圾袋" in old:
                parts.append(
                    "抽绳"
                    if hit("drawstring|draw.string", t)
                    else "手提耳"
                    if hit("handle tie|handles|tie.handle", t)
                    else "平口或未注明"
                )
                parts.append("可堆肥" if hit("compostable", t) else "普通材料")
            if "餐盘" in old or "餐碗" in old:
                n = re.search(r"\b([2-5])\s*compartment", t)
                parts.append(n[1] + "分隔" if n else "无分隔或未注明")
            if old == "食品过滤纱布":
                parts.append("成型过滤袋" if hit("bag", t) else "平面纱布")
            if "真空压缩袋" in old:
                # A vacuum attachment should never inherit a vacuum storage bag direction.
                if not hit(r"\bbags?\b", t):
                    fallback[(tuple(categories(x["category"])), x["seller_id"])].append(
                        x
                    )
                    continue
                parts.append(
                    "手动泵随附"
                    if hit("hand pump|manual pump", t)
                    else "袋体或泵未注明"
                )
            records[x["asin"]] = {
                "direction": " / ".join(dict.fromkeys(old.split(" / ") + parts)),
                "basis": "沿用既有方向并核对封口、分隔等核心结构",
                "rule": "既有方向结构细分",
            }
            continue
        fallback[(tuple(categories(x["category"])), x["seller_id"])].append(x)
    # Exact normalized titles only; no speculative fuzzy merging of residual items.
    for (cats, seller), items in fallback.items():
        for x in items:
            fp = fingerprint(x, headers)
            raw = dict(zip(headers, x["source"]["values"]))
            m = material(x["title"], raw)
            suffix = fp if len(fp) > 15 else x["asin"]
            records[x["asin"]] = {
                "direction": "同款标题："
                + " / ".join(cats)
                + " / "
                + m
                + " / "
                + suffix,
                "basis": "同一细分类目、主体材质一致，英文标题仅去品牌、颜色、尺寸与数量后完全一致",
                "rule": "同款标题精确归一",
            }
    return records


def seller_identity(raw):
    name = str(
        raw.get("BuyBox卖家") or raw.get("卖家名称") or raw.get("Seller Name") or ""
    ).strip()
    identity = (
        str(raw.get("卖家ID") or raw.get("Seller ID") or raw.get("SellerId") or "")
        .strip()
        .upper()
    )
    if not re.fullmatch(r"[A-Z0-9]{8,32}", identity):
        identity = ""
        url = urlparse(str(raw.get("卖家首页") or raw.get("Seller URL") or ""))
        if re.fullmatch(
            r"(?:www\.)?amazon\.(?:com|ca|com.mx|co.uk|de|fr|it|es|co.jp|com.au|in|ae|sa|com.br|nl|se|pl|com.be|com.tr|sg)",
            url.hostname or "",
        ):
            params = parse_qs(url.query)
            candidate = (
                str((params.get("seller") or params.get("me") or [""])[0])
                .strip()
                .upper()
            )
            if re.fullmatch(r"[A-Z0-9]{8,32}", candidate):
                identity = candidate
    return identity, name


def grouping_details(raw, base_direction=""):
    title = str(raw.get("商品标题") or raw.get("Title") or "")
    category = str(raw.get("小类目") or "")
    asin = str(raw.get("ASIN") or "")
    seller, _ = seller_identity(raw)
    legacy = base_direction or ""
    if not legacy or legacy == category.replace("\r", "").replace("\n", " / "):
        legacy = "未校准方向：" + asin
    headers = list(raw)
    item = {
        "asin": asin,
        "status": "eligible",
        "title": title,
        "category": category,
        "direction": legacy,
        "seller_id": seller,
        "source": {"values": [raw[k] for k in headers]},
    }
    return assign([item], headers)[asin]


def group_identity(site, direction, seller_id, asin):
    """Seller is mandatory; missing identities never merge across ASINs."""
    return site, direction, seller_id or "unknown:" + asin
