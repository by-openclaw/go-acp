import { BootstrapService } from '../../../../src/bootstrap.service';
import { ProtectDeviceNameResponseCommandParams } from '../../../../src/command/tx/018-protect-device-name-response-message/params';
import { LocaleData } from '../../../../src/common/locale-data/locale-data.model';
import { CommandParamsValidator } from '../../../../src/command/tx/018-protect-device-name-response-message/params.validator';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('Protect Device Name Response Message - CommandParamsValidator', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the validator', () => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 0,
                deviceName: "SMH"
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
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 0,
                deviceName: "SMH"
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
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: -1,
                deviceName: ""
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceId).toBeDefined();
            expect(errors.deviceId.id).toBe(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.deviceName).toBeDefined();
            expect(errors.deviceName.id).toBe(CommandErrorsKeys.DEVICE_NAME_IS_OUT_OF_RANGE_ERROR_MSG);
        });

        it('Should return errors if params are out of range > MAX', () => {
            // Arrange
            const params: ProtectDeviceNameResponseCommandParams = {
                deviceId: 1024,
                deviceName: ""
            };

            // Act
            const validator = new CommandParamsValidator(params);
            const errors: Record<string, LocaleData> = validator.validate();

            // Assert
            expect(errors.deviceId).toBeDefined();
            expect(errors.deviceId.id).toBe(CommandErrorsKeys.DEVICE_IS_OUT_OF_RANGE_ERROR_MSG);
            expect(errors.deviceName).toBeDefined();
            expect(errors.deviceName.id).toBe(CommandErrorsKeys.DEVICE_NAME_IS_OUT_OF_RANGE_ERROR_MSG);
        });
    });
});
