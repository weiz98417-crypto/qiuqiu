import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/widgets/mobile_theme_canvas.dart';

void main() {
  testWidgets('desktop keeps the themed canvas at phone width', (tester) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const MaterialApp(
      home: Scaffold(
        body: MobileThemeCanvas(
          backgroundAsset: 'assets/images/stadium-sunset.png',
          overlayColor: Colors.transparent,
          child: SizedBox.expand(),
        ),
      ),
    ));

    expect(tester.getSize(find.byType(Image)).width, 520);
  });

  testWidgets('phone gives the theme the full viewport width', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(const MaterialApp(
      home: Scaffold(
        body: MobileThemeCanvas(
          backgroundAsset: 'assets/images/stadium-stage.png',
          overlayColor: Colors.transparent,
          child: SizedBox.expand(),
        ),
      ),
    ));

    expect(tester.getSize(find.byType(Image)).width, 390);
  });
}
