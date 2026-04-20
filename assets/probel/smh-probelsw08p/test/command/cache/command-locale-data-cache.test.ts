import 'reflect-metadata';
import createMockInstance from 'jest-create-mock-instance';
import { LoggingService } from '../../../src/common/logging/logging.service';
import { LocaleDataCache } from '../../../src/command/command-locale-data-cache';

describe('LocaleDataCache', () => {
    let loggingService: jest.Mocked<LoggingService>;
    const localeId = 'en';
    const localeDataCache = new LocaleDataCache();

    beforeAll(async () => {
        loggingService = createMockInstance(LoggingService);
        await localeDataCache.loadLocaleDataAsync(localeId);
    });

    describe('INSTANCE', () => {
        it('should return the same class instance', () => {

            // Arrange

            // Act
            const singleton = LocaleDataCache.INSTANCE;
            const targteSingleton = LocaleDataCache.INSTANCE;

            // Assert
            expect(targteSingleton).toEqual(singleton);
        });
    });

    describe('loadLocaleDataAsync', () => {
        it('should load the caches and get getCommandErrorLocaleData()', () => {
            console.log('test');

            // Arrange
            const localeDataKey = 'MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG';
            const expectedDescription = 'MatrixId argument is out of range ! Allowed range value [0, 255]';

            // Act
            const targetLocaleData = localeDataCache.getCommandErrorLocaleData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });

        it('should load the caches and get getErrorLocaleData()', () => {
            // Arrange
            const localeDataKey = 'EXTENDED_GENERAL_COMMAND_RECEIVED_INFO_MSG';
            const expectedDescription = 'Extended General Command Received';

            // Act
            const targetLocaleData = localeDataCache.getErrorLocaleData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });

        it('should load the caches and get getRxExtendedCommandLocaleData()', () => {
            // Arrange
            const localeDataKey = 'CROSSPOINT_INTERROGATE_MESSAGE';
            const expectedDescription = 'This message is a request for Tally information by matrix no., level and destination, issued by the remote device.\nThe controller will respond to this message with an EXTENDED CROSSPOINT TALLY message (Command Byte 0x83).';

            // Act
            const targetLocaleData = localeDataCache.getRxExtendedCommandLocaleData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });

        it('should load the caches and get getRxGeneralCommandLocalData()', async () => {
            // Arrange
            const localeDataKey = 'CROSSPOINT_INTERROGATE_MESSAGE';
            const expectedDescription = 'This message is a request for Tally information by matrix no., level and destination, issued by the remote device.\nThe controller will respond to this message with a CROSSPOINT TALLY message (normal or extended) (Command Bytes 0x03 or 0x83).';

            // Act
            const targetLocaleData = localeDataCache.getRxGeneralCommandLocalData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });

        it('should load the caches and get getTxExtendedCommandData()', () => {
            // Arrange
            const localeDataKey = 'CROSSPOINT_TALLY_MESSAGE';
            const expectedDescription = 'This message returns router tally information in response to an EXTENDED CROSSPOINT INTERROGATE message (Command Byte 0x81).';

            // Act
            const targetLocaleData = localeDataCache.getTxExtendedCommandData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });

        it('should load the caches and get getTxGeneralCommandLocaleData()', () => {
            // Arrange
            const localeDataKey = 'CROSSPOINT_TALLY_MESSAGE';
            const expectedDescription = 'This message returns router tally information in response to a CROSSPOINT INTERROGATE message (Command Byte 0x01).';

            // Act
            const targetLocaleData = localeDataCache.getTxGeneralCommandLocaleData(localeDataKey);

            // Assert
            expect(targetLocaleData.description).toContain(expectedDescription);
        });
    });
});
