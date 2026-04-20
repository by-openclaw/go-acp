import { BootstrapService } from '../../../../src/bootstrap.service';
import { ProtectDeviceNameRequestMessageCommandParams } from '../../../../src/command/rx/017-protect-device-name-request-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/rx/017-protect-device-name-request-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Device Name Request Message Command - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 0
            };

            // Act
            const validator = new CommandParamsValidator(params);

            // Assert
            expect(validator).toBeDefined();
            expect(validator.data).toBe(params);
        });
    });

    describe('validate', () => {
        it('Should succeed with valid params - ...', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 907
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors).toBeDefined();
            expect(Object.keys(errors).length).toBe(0);
        });

        it('Should return errors id params are out of range < MIN', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: -1
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceId).toBeDefined();
            expect(errors.deviceId.id).toBe(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: ProtectDeviceNameRequestMessageCommandParams = {
                deviceId: 1024
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceId).toBeDefined();
            expect(errors.deviceId.id).toBe(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
