import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/theme/app_theme.dart';
import 'package:qiuqiu/widgets/metal_button.dart';

BoxDecoration _buttonDecoration(WidgetTester tester) =>
    tester.widget<Ink>(find.byType(Ink)).decoration! as BoxDecoration;

void main() {
  testWidgets(
      'MetalButton fills with solid championBlue and an 8px radius (DESIGN.md)',
      (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: MetalButton(onPressed: () {}, child: const Text('开始陪看')),
    ));

    final decoration = _buttonDecoration(tester);
    expect(decoration.gradient, isNull, reason: 'DESIGN.md：不使用渐变按钮');
    expect(decoration.color, AppColors.championBlue);
    expect(decoration.borderRadius, BorderRadius.circular(8));
    expect(
      tester.widget<InkWell>(find.byType(InkWell)).borderRadius,
      BorderRadius.circular(8),
    );
  });

  testWidgets('disabled MetalButton also stays solid with an 8px radius',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: MetalButton(onPressed: null, child: Text('开始陪看')),
    ));

    final decoration = _buttonDecoration(tester);
    expect(decoration.gradient, isNull);
    expect(decoration.color, isNot(AppColors.championBlue));
    expect(decoration.borderRadius, BorderRadius.circular(8));
  });
}
