import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/main.dart';

void main() {
  testWidgets('App renders', (WidgetTester tester) async {
    await tester.pumpWidget(const QiuQiuApp());
    expect(find.text('球球陪你'), findsOneWidget);
  });
}
